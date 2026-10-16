package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	c, err := Parse([]byte(`
rooms:
  - id: sala-a
    title: Keynote
    source: en
    targets: [es, pt]
    glossary: [Kubernetes, Nerdearla]
  - id: sala-b
    source: es
    targets: [en]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Rooms) != 2 {
		t.Fatalf("rooms = %d, want 2", len(c.Rooms))
	}
	a := c.Rooms[0]
	if a.ID != "sala-a" || a.Title != "Keynote" || a.Source != "en" ||
		strings.Join(a.Targets, ",") != "es,pt" || len(a.Glossary) != 2 {
		t.Fatalf("room a = %+v", a)
	}
	if c.Rooms[1].Title != "sala-b" {
		t.Fatalf("missing title should default to id, got %q", c.Rooms[1].Title)
	}
}

func TestParseOmittedTargetsIsEmptyList(t *testing.T) {
	c, err := Parse([]byte("rooms:\n  - id: a\n    source: en\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Rooms[0].Targets == nil {
		t.Fatal("omitted targets must be an empty, non-nil list (it is served as JSON)")
	}
	b, _ := json.Marshal(c.Rooms[0])
	if !strings.Contains(string(b), `"targets":[]`) {
		t.Fatalf("json = %s", b)
	}
}

func TestParseMoreLanguages(t *testing.T) {
	c, err := Parse([]byte("rooms:\n  - id: a\n    source: fr\n    targets: [de, it, es]\n"))
	if err != nil || strings.Join(c.Rooms[0].Languages(), ",") != "fr,de,it,es" {
		t.Fatalf("got %+v, %v", c, err)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no rooms":         `rooms: []`,
		"bad id":           "rooms:\n  - id: Sala A\n    source: en\n",
		"duplicate id":     "rooms:\n  - id: a\n    source: en\n  - id: a\n    source: es\n",
		"bad source":       "rooms:\n  - id: a\n    source: xx\n",
		"bad target":       "rooms:\n  - id: a\n    source: en\n    targets: [klingon]\n",
		"target is source": "rooms:\n  - id: a\n    source: en\n    targets: [en]\n",
		"duplicate target": "rooms:\n  - id: a\n    source: en\n    targets: [es, fr, es]\n",
		"huge glossary":    "rooms:\n  - id: a\n    source: en\n    glossary: [" + strings.Repeat("x, ", maxGlossary) + "x]\n",
		"not yaml":         "rooms: [",
		"short pin":        "rooms:\n  - id: a\n    source: en\n    audience_pin: \"12\"\n",
		"letters in pin":   "rooms:\n  - id: a\n    source: en\n    audience_pin: \"12ab\"\n",
		"bad exports":      "rooms:\n  - id: a\n    source: en\n    exports: everyone\n",
		"negative days":    "rooms:\n  - id: a\n    source: en\n    retention_days: -1\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(in)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestPrivacySettings(t *testing.T) {
	c, err := Parse([]byte("rooms:\n  - id: a\n    source: hi\n  - id: b\n    source: hi\n    audience_pin: \"4821\"\n    exports: admin\n    store_transcript: false\n    retention_days: 30\n"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := c.Rooms[0], c.Rooms[1]
	if !a.Stores() || a.ExportPolicy() != ExportReaders || a.AudiencePIN != "" {
		t.Errorf("defaults: %+v", a)
	}
	if b.Stores() || b.ExportPolicy() != ExportAdmin || b.AudiencePIN != "4821" || b.RetentionDays != 30 {
		t.Errorf("set: %+v", b)
	}
}
