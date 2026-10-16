// Copyright 2026 the Dubash contributors. Apache-2.0.

package eventdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogPersistsWithoutYAML(t *testing.T) {
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("events_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	path := filepath.Join(t.TempDir(), "rooms.yaml")
	if err = os.WriteFile(path, []byte("rooms:\n - id: event\n   source: hi\n   targets: [en]\n   asr_mode: codemix\n   prompt: Speaker vocabulary\n   always_translate: [en]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Catalog(ctx, db, path)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	cfg, err = Catalog(ctx, db, path)
	if err != nil {
		t.Fatalf("database still depends on YAML: %v", err)
	}
	if cfg.Rooms[0].ASRMode != "codemix" || cfg.Rooms[0].Prompt != "Speaker vocabulary" || len(cfg.Rooms[0].AlwaysTranslate) != 1 {
		t.Fatalf("private configuration fields lost: %+v", cfg)
	}
	cfg.Rooms[0].Title = "Updated event"
	if err = Replace(ctx, db, cfg); err != nil {
		t.Fatal(err)
	}
	added := config.Room{ID: "browser-event", Title: "Browser event", Source: "hi", Targets: []string{"en"}, AudiencePIN: "1234", RetentionDays: 30, Exports: "admin"}
	if err = Add(ctx, db, added); err != nil {
		t.Fatal(err)
	}
	if err = Add(ctx, db, added); err == nil {
		t.Fatal("duplicate browser event accepted")
	}
	if err = Replace(ctx, db, config.Config{}); err == nil {
		t.Fatal("invalid catalog accepted")
	}
	cfg, err = Catalog(ctx, db, path)
	if err != nil || cfg.Rooms[0].Title != "Updated event" || len(cfg.Rooms) != 2 || cfg.Rooms[1].AudiencePIN != "1234" {
		t.Fatalf("atomic import: %+v %v", cfg, err)
	}
}
