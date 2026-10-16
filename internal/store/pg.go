// Copyright 2026 the Dubash contributors. Apache-2.0.

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/UtsavInTech/dubash/internal/caption"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OpenPG stores transcripts centrally so room owners can restart on a different
// machine. The primary key makes retries idempotent; older revisions never
// overwrite corrections. File transcripts are not implicitly migrated.
func OpenPG(ctx context.Context, url string) (*Store, error) {
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
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7264)`); err == nil {
		_, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS dubash_segments (
 room text NOT NULL, id bigint NOT NULL, rev integer NOT NULL,
 payload jsonb NOT NULL, PRIMARY KEY(room,id)
 )`)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		tx.Rollback(context.WithoutCancel(ctx))
		db.Close()
		return nil, fmt.Errorf("transcript schema: %w", err)
	}

	return &Store{pg: db}, nil
}

func (s *Store) Close() {
	if s.pg != nil {
		s.pg.Close()
	}
}

func (s *Store) appendPG(seg caption.Segment) error {
	data, err := json.Marshal(seg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = s.pg.Exec(ctx, `INSERT INTO dubash_segments(room,id,rev,payload) VALUES($1,$2,$3,$4)
 ON CONFLICT(room,id) DO UPDATE SET rev=EXCLUDED.rev,payload=EXCLUDED.payload
 WHERE EXCLUDED.rev >= dubash_segments.rev`, seg.Room, seg.ID, seg.Rev, data)
	return err
}

func (s *Store) loadPG(room string) ([]caption.Segment, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := s.pg.Query(ctx, `SELECT payload FROM dubash_segments WHERE room=$1 ORDER BY id`, room)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []caption.Segment
	for rows.Next() {
		var data []byte
		var seg caption.Segment
		if err = rows.Scan(&data); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal(data, &seg); err != nil {
			return nil, 0, err
		}
		out = append(out, seg)
	}
	return out, 0, rows.Err()
}

// Ready checks the shared transcript database, or succeeds for file storage.
func (s *Store) Ready(ctx context.Context) error {
	if s.pg != nil {
		return s.pg.Ping(ctx)
	}
	return nil
}
