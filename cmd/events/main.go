// Copyright 2026 the Dubash contributors. Apache-2.0.

// Command events explicitly imports a room catalog into PostgreSQL.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/eventdb"
	"os"
	"time"
)

func main() {
	path := flag.String("import", "", "validated rooms YAML to replace the database catalog with")
	flag.Parse()
	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(path string) error {
	if path == "" || os.Getenv("DATABASE_URL") == "" {
		return fmt.Errorf("use -import rooms.yaml and set DATABASE_URL; restart room owners after import")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := eventdb.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err = eventdb.Replace(ctx, db, cfg); err != nil {
		return err
	}
	fmt.Printf("Imported %d events. Restart room owners to activate the catalog.\n", len(cfg.Rooms))
	return nil
}
