// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package eventdb persists the event catalog independently of the vocabulary bank.
package eventdb

import (
	"context"
	"fmt"

	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

const schema = `CREATE TABLE IF NOT EXISTS dubash_events (
 id text PRIMARY KEY, definition bytea NOT NULL, position integer NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now()
); CREATE TABLE IF NOT EXISTS dubash_event_catalog (singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton));`

// Open migrates under a transaction-scoped lock, safe for simultaneous startups.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		db.Close()
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7263)`); err == nil {
		_, err = tx.Exec(ctx, schema)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		tx.Rollback(context.WithoutCancel(ctx))
		db.Close()
		return nil, fmt.Errorf("event catalog: %w", err)
	}
	return db, nil
}

// Catalog returns the database catalog. The seed file is read only once, on
// first initialization; subsequent boots do not need rooms.yaml at all.
func Catalog(ctx context.Context, db *pgxpool.Pool, seedPath string) (config.Config, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return config.Config{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7263)`); err != nil {
		return config.Config{}, err
	}
	var initialized bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dubash_event_catalog)`).Scan(&initialized); err != nil {
		return config.Config{}, err
	}
	if !initialized {
		seed, err := config.Load(seedPath)
		if err != nil {
			return config.Config{}, fmt.Errorf("initialize event catalog: %w", err)
		}
		if err = replace(ctx, tx, seed); err != nil {
			return config.Config{}, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT definition FROM dubash_events ORDER BY position, id`)
	if err != nil {
		return config.Config{}, err
	}
	var cfg config.Config
	for rows.Next() {
		var data []byte
		var r config.Room
		if err = rows.Scan(&data); err != nil {
			rows.Close()
			return cfg, err
		}
		if err = yaml.Unmarshal(data, &r); err != nil {
			rows.Close()
			return cfg, err
		}
		cfg.Rooms = append(cfg.Rooms, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return cfg, err
	}
	// Apply the same validation and defaults as file-backed catalogs.
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	cfg, err = config.Parse(data)
	if err != nil {
		return cfg, err
	}
	if err = tx.Commit(ctx); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Replace atomically imports a complete, validated catalog. Restart room owners
// afterwards; live room configuration is intentionally immutable.
func Replace(ctx context.Context, db *pgxpool.Pool, cfg config.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	cfg, err = config.Parse(data)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7263)`); err != nil {
		return err
	}
	if err = replace(ctx, tx, cfg); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func replace(ctx context.Context, tx pgx.Tx, cfg config.Config) error {
	if _, err := tx.Exec(ctx, `DELETE FROM dubash_events`); err != nil {
		return err
	}
	for i, r := range cfg.Rooms {
		data, err := yaml.Marshal(r)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO dubash_events(id, definition, position) VALUES($1,$2,$3)`, r.ID, data, i); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO dubash_event_catalog(singleton) VALUES(true) ON CONFLICT DO NOTHING`)
	return err
}

// Add persists an additional event without replacing existing definitions.
func Add(ctx context.Context, db *pgxpool.Pool, rc config.Room) error {
	data, err := yaml.Marshal(config.Config{Rooms: []config.Room{rc}})
	if err != nil {
		return err
	}
	validated, err := config.Parse(data)
	if err != nil {
		return err
	}
	data, err = yaml.Marshal(validated.Rooms[0])
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7263)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO dubash_events(id,definition,position) SELECT $1,$2,COALESCE(MAX(position),-1)+1 FROM dubash_events`, rc.ID, data); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
