package store

import (
	"os"
	"path/filepath"
	"testing"

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
