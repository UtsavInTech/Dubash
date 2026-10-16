package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/caption"
)

func TestAppendAndLoad(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := s.Load("sala-a"); err != nil || got != nil {
		t.Fatalf("empty room: got %v, %v", got, err)
	}
	want := []caption.Segment{
		{ID: 0, Room: "sala-a", T0: 0, T1: 1.5, Lang: "en", Text: "Hello.", Translations: map[string]string{"es": "Hola."}},
		{ID: 1, Room: "sala-a", T0: 1.5, T1: 3, Lang: "en", Text: "Bye.", Error: "boom"},
	}
	for _, seg := range want {
		if err := s.Append(seg); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := s.Load("sala-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Translations["es"] != "Hola." || got[1].Error != "boom" || got[1].T1 != 3 {
		t.Fatalf("got %+v", got)
	}
	if other, _, _ := s.Load("sala-b"); other != nil {
		t.Fatalf("rooms must be isolated, got %+v", other)
	}
}

func TestLoadSkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	good := `{"id":0,"room":"sala-a","t0":0,"t1":1,"lang":"en","text":"Hello."}`
	good2 := `{"id":1,"room":"sala-a","t0":1,"t1":2,"lang":"en","text":"Bye."}`
	data := good + "\nnot json at all\n" + good2 + "\n" + `{"id":2,"room":"sala-a","t0":2,"te`
	if err := os.WriteFile(filepath.Join(dir, "sala-a.jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	got, skipped, err := s.Load("sala-a")
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2", skipped)
	}
	if len(got) != 2 || got[0].Text != "Hello." || got[1].Text != "Bye." {
		t.Fatalf("got %+v", got)
	}
}

func TestTranscriptsArePrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(caption.Segment{ID: 1, Room: "hall", Text: "नमस्ते"}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, "hall.jsonl"): 0o600} {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: mode %v, %v; want %v", path, fi.Mode().Perm(), err, want)
		}
	}
}

func TestDisableDeleteAndPurge(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.Disable("secret")
	s.Append(caption.Segment{ID: 1, Room: "secret", Text: "off the record"})
	if segs, _, _ := s.Load("secret"); len(segs) != 0 {
		t.Fatalf("a room that keeps no transcript stored %+v", segs)
	}

	for _, seg := range []caption.Segment{
		{ID: 1, Room: "hall", Text: "old", At: now.AddDate(0, 0, -10)},
		{ID: 2, Room: "hall", Text: "no time"},
		{ID: 3, Room: "hall", Text: "new", At: now},
		{ID: 1, Room: "hall", Rev: 1, Text: "old, corrected", At: now.AddDate(0, 0, -10)},
	} {
		if err := s.Append(seg); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Purge("hall", now.AddDate(0, 0, -7))
	if err != nil || n != 1 {
		t.Fatalf("purged %d, %v; want 1", n, err)
	}
	segs, _, _ := s.Load("hall")
	if len(segs) != 2 || segs[0].Text != "no time" || segs[1].Text != "new" {
		t.Fatalf("after purge: %+v", segs)
	}
	if n, _ := s.Purge("hall", now.AddDate(0, 0, -7)); n != 0 {
		t.Errorf("second purge dropped %d", n)
	}
	if err := s.Delete("hall"); err != nil {
		t.Fatal(err)
	}
	if segs, _, _ := s.Load("hall"); segs != nil {
		t.Fatalf("after delete: %+v", segs)
	}
	if err := s.Delete("hall"); err != nil {
		t.Errorf("deleting twice: %v", err)
	}
}
