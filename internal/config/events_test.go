package config

import (
	"path/filepath"
	"testing"
)

func TestCreatedEventsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.yaml")
	seed := Config{Rooms: []Room{{ID: "seed", Title: "Seed", Source: "hi"}}}
	cfg, err := WithCreated(seed, path)
	if err != nil || len(cfg.Rooms) != 1 {
		t.Fatal(cfg, err)
	}
	event := Room{ID: "community", Title: "Community", Source: "hi", Targets: []string{"en"}, AudiencePIN: "1234", RetentionDays: 30, Exports: "admin"}
	if err := AddCreated(path, event); err != nil {
		t.Fatal(err)
	}
	cfg, err = WithCreated(seed, path)
	if err != nil || len(cfg.Rooms) != 2 || cfg.Rooms[1].AudiencePIN != "1234" || cfg.Rooms[1].RetentionDays != 30 {
		t.Fatalf("lost event/privacy fields: %+v %v", cfg, err)
	}
	if err := AddCreated(path, event); err == nil {
		t.Fatal("duplicate persisted")
	}
	if err := AddCreated(path, Room{ID: "invalid", Source: "wrong"}); err == nil {
		t.Fatal("invalid event persisted")
	}
	cfg, err = WithCreated(seed, path)
	if err != nil || len(cfg.Rooms) != 2 {
		t.Fatal("invalid save corrupted catalog", cfg, err)
	}
}
