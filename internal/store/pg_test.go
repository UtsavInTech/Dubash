// Copyright 2026 the Dubash contributors. Apache-2.0.

package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/caption"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresTranscriptRestartAndRevision(t *testing.T) {
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL integration")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := fmt.Sprintf("transcript_test_%d", time.Now().UnixNano())
	if _, err = db.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	first, err := OpenPG(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	seg := caption.Segment{Room: "main", ID: 42, Epoch: 3, Rev: 1, Text: "corrected", Translations: map[string]string{"hi": "सही"}}
	if err = first.Append(seg); err != nil {
		t.Fatal(err)
	}
	stale := seg
	stale.Rev = 0
	stale.Text = "stale"
	if err = first.Append(stale); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := OpenPG(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, skipped, err := second.Load("main")
	if err != nil || skipped != 0 || len(got) != 1 || got[0].Text != "corrected" || got[0].Translations["hi"] != "सही" {
		t.Fatalf("restart lost current revision: %+v %d %v", got, skipped, err)
	}
}
