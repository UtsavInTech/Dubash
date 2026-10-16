// Copyright 2026 the Dubash contributors. Apache-2.0.

package bank

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func approved(scope Scope, id, term string, mod ...func(*Entry)) Entry {
	e := Entry{Scope: scope, ScopeID: id, Term: term, Status: Approved}
	for _, m := range mod {
		m(&e)
	}
	return e
}

func TestValidate(t *testing.T) {
	e := Entry{Scope: ScopeSpeaker, ScopeID: "Dhirendra Krishna Shastri", Term: "  बागेश्वर   धाम ",
		Aliases: []string{"बागेश्वर धाम", " Bageshwar Dham ", "bageshwar dham"}, Translations: map[string]string{"te": " బాగేశ్వర్ ధామ్ ", "kn": " "}}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	if e.Term != "बागेश्वर धाम" || e.ScopeID != "dhirendra-krishna-shastri" || e.Status != Suggested ||
		!slices.Equal(e.Aliases, []string{"Bageshwar Dham"}) || len(e.Translations) != 1 || e.Translations["te"] != "బాగేశ్వర్ ధామ్" {
		t.Errorf("normalized to %+v", e)
	}
	for _, bad := range []Entry{
		{Scope: ScopeEvent, ScopeID: "a"},
		{Scope: "room", ScopeID: "a", Term: "x"},
		{Scope: ScopeEvent, Term: "x"},
		{Scope: ScopeEvent, ScopeID: "a", Term: "x", Lang: "xx"},
		{Scope: ScopeEvent, ScopeID: "a", Term: "x", Translations: map[string]string{"zz": "y"}},
		{Scope: ScopeEvent, ScopeID: "a", Term: strings.Repeat("x", 121)},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if Fold("फ़ोन  Phone") != Fold("फोन phone") {
		t.Error("Fold must ignore nukta, case and spaces")
	}
}

// TestResolvePrecedence: event over speaker over organisation; only
// approved entries; other events and speakers ignored.
func TestResolvePrecedence(t *testing.T) {
	entries := []Entry{
		approved(ScopeOrg, DefaultOrg, "Dubash", func(e *Entry) { e.Meaning = "org" }),
		approved(ScopeSpeaker, "shastri", "Dubash", func(e *Entry) { e.Meaning = "speaker" }),
		approved(ScopeEvent, "hall", "Dubash", func(e *Entry) { e.Meaning = "event" }),
		approved(ScopeSpeaker, "shastri", "बालाजी"),
		approved(ScopeOrg, DefaultOrg, "Sarvam"),
		approved(ScopeEvent, "other-hall", "Elsewhere"),
		approved(ScopeSpeaker, "someone-else", "Not this speaker"),
		{Scope: ScopeEvent, ScopeID: "hall", Term: "Unapproved", Status: Suggested},
		approved(ScopeEvent, "hall", "dubash", func(e *Entry) { e.Lang = "hi"; e.Meaning = "hi event" }),
	}
	got := Resolve(entries, "hall", "Shastri")
	var terms []string
	for _, e := range got {
		terms = append(terms, e.Term+"/"+e.Meaning)
	}
	want := []string{"Dubash/event", "dubash/hi event", "बालाजी/", "Sarvam/"}
	if !slices.Equal(terms, want) {
		t.Errorf("resolved %v, want %v", terms, want)
	}
}

func TestContextHintsAndGlossary(t *testing.T) {
	v := NewVersion([]Entry{
		approved(ScopeEvent, "hall", "राम कथा", func(e *Entry) {
			e.Lang = "hi"
			e.Aliases = []string{"रामकथा"}
			e.Translations = map[string]string{"te": "రామకథ"}
		}),
		approved(ScopeEvent, "hall", "Dubash", func(e *Entry) { e.Keep = true }),
		approved(ScopeOrg, DefaultOrg, "Sarvam AI", func(e *Entry) { e.Keep = true; e.Aliases = []string{"Sarvam"} }),
		approved(ScopeEvent, "hall", "Rehearsal", func(e *Entry) { e.Lang = "en" }),
	}, "hall", "", "test", "")
	c := NewContext(v, []string{"QR code", "Dubash"})
	if h := c.Hints("hi"); !slices.Equal(h, []string{"Dubash", "राम कथा", "रामकथा", "Sarvam AI", "Sarvam", "QR code"}) {
		t.Errorf("hints %q", h)
	}
	g := c.Glossary("hi", "te")
	if len(g.Terms) != 1 || g.Terms[0].Target != "రామకథ" || !slices.Equal(g.Terms[0].Source, []string{"राम कथा", "रामकथा"}) {
		t.Errorf("terms %+v", g.Terms)
	}
	if !slices.Equal(g.Keep, []string{"Dubash", "Sarvam AI", "Sarvam", "QR code"}) {
		t.Errorf("keep %q", g.Keep)
	}
	if g := c.Glossary("hi", "kn"); len(g.Terms) != 0 {
		t.Errorf("Kannada has no approved translation: %+v", g)
	}
	var nilCtx *Context
	if nilCtx.Hints("hi") != nil || !nilCtx.Glossary("hi", "te").Empty() {
		t.Error("a nil context must be empty")
	}
}

func TestSuggest(t *testing.T) {
	got := Suggest("हम दो चार महीने में सर्वम एआई का एक बार डबस पायलट देखेंगे।",
		"हम 2-4 महीने में Sarvam AI का एक बार Dubash pilot देखेंगे।", "hi", "hall")
	var pairs []string
	for _, e := range got {
		pairs = append(pairs, e.Misheard[0]+" → "+e.Term)
		if e.Status != Suggested || e.Scope != ScopeEvent || e.ScopeID != "hall" || e.Source != SourceCorrection {
			t.Errorf("suggestion %+v", e)
		}
	}
	// "दो चार" → "2-4" is a number change: no suggestion.
	want := []string{"सर्वम एआई → Sarvam AI", "डबस पायलट → Dubash pilot"}
	if !slices.Equal(pairs, want) {
		t.Errorf("suggested %q, want %q", pairs, want)
	}
	if s := Suggest("Hello, world.", "hello world", "en", "hall"); len(s) != 0 {
		t.Errorf("punctuation and case alone suggested %+v", s)
	}
	if s := Suggest("a b c", "a completely different sentence now said here", "en", "h"); len(s) != 0 {
		t.Errorf("a rewrite suggested %+v", s)
	}
}

// stores runs fn against every store: the file store always, PostgreSQL
// when DUBASH_TEST_PG names a database (it is wiped).
func stores(t *testing.T, fn func(t *testing.T, s Store)) {
	t.Run("file", func(t *testing.T) {
		s, err := OpenFile(filepath.Join(t.TempDir(), "bank.json"))
		if err != nil {
			t.Fatal(err)
		}
		fn(t, s)
	})
	url := os.Getenv("DUBASH_TEST_PG")
	if url == "" {
		t.Log("DUBASH_TEST_PG not set: PostgreSQL store not tested")
		return
	}
	t.Run("postgres", func(t *testing.T) {
		ctx := context.Background()
		s, err := OpenPG(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if _, err := s.db.Exec(ctx, `TRUNCATE vocabulary_entries, vocabulary_translations, event_context_pins, event_context_versions, transcript_corrections, audit_log RESTART IDENTITY CASCADE`); err != nil {
			t.Fatal(err)
		}
		fn(t, s)
	})
}

func TestStoreContract(t *testing.T) {
	stores(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		e, err := s.SaveEntry(ctx, Entry{Scope: ScopeEvent, ScopeID: "hall", Term: "राम कथा", Lang: "hi",
			Misheard: []string{"राम कता"}, Translations: map[string]string{"te": "రామకథ"}}, "admin")
		if err != nil {
			t.Fatal(err)
		}
		if e.ID == "" || e.Version != 1 || e.Status != Suggested {
			t.Fatalf("created %+v", e)
		}
		if _, err := s.SaveEntry(ctx, Entry{Scope: ScopeEvent, ScopeID: "hall"}, "admin"); err == nil {
			t.Fatal("saved an entry without a term")
		}
		e.Aliases = []string{"रामकथा"}
		e2, err := s.SaveEntry(ctx, e, "admin")
		if err != nil || e2.Version != 2 || e2.ID != e.ID {
			t.Fatalf("update: %+v %v", e2, err)
		}
		cur, err := s.Entry(ctx, e.ID)
		if err != nil || cur.Version != 2 || !slices.Equal(cur.Aliases, []string{"रामकथा"}) || cur.Translations["te"] != "రామకథ" {
			t.Fatalf("current %+v %v", cur, err)
		}
		hist, err := s.History(ctx, e.ID)
		if err != nil || len(hist) != 2 || len(hist[0].Aliases) != 0 {
			t.Fatalf("history %+v %v", hist, err)
		}
		if _, err := s.Entry(ctx, "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unknown entry: %v", err)
		}
		if _, err := s.SaveEntry(ctx, Entry{ID: "nope", Scope: ScopeOrg, Term: "x"}, "admin"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("update of unknown entry: %v", err)
		}

		// Approve, and a duplicate suggestion merges into it.
		if _, err := Approve(ctx, s, e.ID, "admin"); err != nil {
			t.Fatal(err)
		}
		dup, _ := s.SaveEntry(ctx, Entry{Scope: ScopeEvent, ScopeID: "hall", Term: "राम कथा", Lang: "hi", Misheard: []string{"राम खता"}}, "admin")
		merged, err := Approve(ctx, s, dup.ID, "admin")
		if err != nil || merged.ID != e.ID || !slices.Equal(merged.Misheard, []string{"राम कता", "राम खता"}) {
			t.Fatalf("merge: %+v %v", merged, err)
		}
		if d, _ := s.Entry(ctx, dup.ID); d.Status != Retired {
			t.Fatalf("duplicate left as %s", d.Status)
		}
		s.SaveEntry(ctx, Entry{Scope: ScopeOrg, Term: "Dubash", Keep: true, Status: Approved}, "admin")
		all, err := s.Entries(ctx, Filter{})
		if err != nil || len(all) != 3 {
			t.Fatalf("entries %d %v", len(all), err)
		}
		if got, _ := s.Entries(ctx, Filter{Status: Approved}); len(got) != 2 || got[0].Scope != ScopeEvent {
			t.Fatalf("approved, event first: %+v", got)
		}
		if got, _ := s.Entries(ctx, Filter{Query: "खता", Status: Approved}); len(got) != 1 {
			t.Fatalf("query on misheard: %+v", got)
		}

		// Versions are numbered per event, immutable, and pinned.
		if _, ok, _ := s.Pinned(ctx, "hall"); ok {
			t.Fatal("pinned before any version")
		}
		v1, err := s.SaveVersion(ctx, NewVersion(all, "hall", "", "admin", "first"), "admin")
		if err != nil || v1.Number != 1 || len(v1.Entries) != 2 {
			t.Fatalf("version 1: %+v %v", v1, err)
		}
		if err := s.Pin(ctx, "hall", v1.ID, "admin"); err != nil {
			t.Fatal(err)
		}
		if err := s.Pin(ctx, "other", v1.ID, "admin"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("pinned another event's version: %v", err)
		}
		// Editing the bank does not change the pinned version.
		s.SaveEntry(ctx, Entry{Scope: ScopeEvent, ScopeID: "hall", Term: "New", Status: Approved}, "admin")
		pinned, ok, err := s.Pinned(ctx, "hall")
		if err != nil || !ok || pinned.ID != v1.ID || len(pinned.Entries) != 2 || pinned.Entries[0].Translations["te"] != "రామకథ" {
			t.Fatalf("pinned %+v %v %v", pinned, ok, err)
		}
		all, _ = s.Entries(ctx, Filter{})
		v2, _ := s.SaveVersion(ctx, NewVersion(all, "hall", "", "admin", ""), "admin")
		if v2.Number != 2 || len(v2.Entries) != 3 || v2.Hash == v1.Hash {
			t.Fatalf("version 2: %+v", v2)
		}
		if vs, _ := s.Versions(ctx, "hall"); len(vs) != 2 || vs[0].Number != 2 || vs[0].Entries != nil {
			t.Fatalf("versions %+v", vs)
		}
		if got, err := s.Version(ctx, v1.ID); err != nil || got.Hash != v1.Hash {
			t.Fatalf("version by id: %+v %v", got, err)
		}

		// Corrections and the suggestions they make.
		c := Correction{Event: "hall", Segment: 7, Rev: 1, Raw: "डबस में स्वागत", Corrected: "Dubash में स्वागत", Lang: "hi", By: "admin"}
		ids, err := SuggestFrom(ctx, s, c, "admin")
		if err != nil || len(ids) != 1 {
			t.Fatalf("suggest: %v %v", ids, err)
		}
		if again, _ := SuggestFrom(ctx, s, c, "admin"); len(again) != 0 {
			t.Fatalf("the same fix suggested twice: %v", again)
		}
		c.Suggested = ids
		if _, err := s.SaveCorrection(ctx, c); err != nil {
			t.Fatal(err)
		}
		cs, err := s.Corrections(ctx, "hall")
		if err != nil || len(cs) != 1 || cs[0].Corrected != c.Corrected || !slices.Equal(cs[0].Suggested, ids) {
			t.Fatalf("corrections %+v %v", cs, err)
		}
		audit, err := s.Audit(ctx, 5)
		if err != nil || len(audit) != 5 || audit[0].Action != "correct" {
			t.Fatalf("audit %+v %v", audit, err)
		}
	})
}

func TestFileStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bank.json")
	s, _ := OpenFile(path)
	e, _ := s.SaveEntry(context.Background(), Entry{Scope: ScopeOrg, Term: "Dubash", Status: Approved}, "admin")
	again, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Entry(context.Background(), e.ID); err != nil || got.Term != "Dubash" {
		t.Fatalf("reopened: %+v %v", got, err)
	}
}
