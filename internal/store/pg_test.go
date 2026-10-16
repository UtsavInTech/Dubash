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

func TestPostgresRetentionDeleteAndDisable(t *testing.T) {
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
	schema := fmt.Sprintf("retention_test_%d", time.Now().UnixNano())
	if _, err = db.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, _ := url.Parse(raw)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	s, err := OpenPG(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	for _, seg := range []caption.Segment{
		{Room: "hall", ID: 1, Text: "old", At: now.AddDate(0, 0, -40)},
		{Room: "hall", ID: 2, Text: "no time"},
		{Room: "hall", ID: 3, Text: "new", At: now},
		{Room: "other", ID: 1, Text: "other room", At: now.AddDate(0, 0, -40)},
	} {
		if err := s.Append(seg); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.Purge("hall", now.AddDate(0, 0, -30)); err != nil || n != 1 {
		t.Fatalf("purged %d, %v; want 1", n, err)
	}
	if got, _, _ := s.Load("hall"); len(got) != 2 || got[0].Text != "no time" || got[1].Text != "new" {
		t.Fatalf("after purge: %+v", got)
	}
	if err := s.Delete("hall"); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Load("hall"); len(got) != 0 {
		t.Fatalf("after delete: %+v", got)
	}
	if got, _, _ := s.Load("other"); len(got) != 1 {
		t.Fatalf("another room lost lines: %+v", got)
	}
	s.Disable("secret")
	s.Append(caption.Segment{Room: "secret", ID: 1, Text: "off the record"})
	if got, _, _ := s.Load("secret"); len(got) != 0 {
		t.Fatalf("a room that keeps no transcript stored %+v", got)
	}
}
