// Copyright 2026 the Dubash contributors. Apache-2.0.

package bank

import (
	"slices"

	"github.com/UtsavInTech/dubash/internal/script"
	"github.com/UtsavInTech/dubash/internal/translate"
)

// Context is a pinned version made ready for the live pipeline: phrase
// hints for recognition, a glossary per target language, and the entries
// for the correction stage. It is immutable; a room swaps the whole
// pointer when a new version is pinned.
type Context struct {
	Version Version
	// Base holds rooms.yaml's glossary terms: hints, and never translated.
	Base []string
	// Index searches the version's script chunks (empty without a script).
	Index *script.Index
}

// NewContext readies v, adding base terms (rooms.yaml) that the bank does
// not cover, and indexing its script.
func NewContext(v Version, base []string) *Context {
	return &Context{Version: v, Base: base, Index: script.NewIndex(v.Script)}
}

// Entries are the version's entries, event entries first.
func (c *Context) Entries() []Entry {
	if c == nil {
		return nil
	}
	return c.Version.Entries
}

// applies reports whether e is meant for speech in spoken.
func applies(e Entry, spoken string) bool { return e.Lang == "" || e.Lang == spoken }

// Hints lists the phrase hints for speech in spoken: terms and aliases,
// event entries first (recognizers take a limited number: Sarvam 50),
// then rooms.yaml's terms, without duplicates.
func (c *Context) Hints(spoken string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if k := Fold(s); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	if c == nil {
		return nil
	}
	for _, e := range c.Version.Entries {
		if applies(e, spoken) {
			for _, s := range e.Spellings() {
				add(s)
			}
		}
	}
	for _, s := range c.Base {
		add(s)
	}
	return out
}

// Glossary is what translating from src into dst must respect: keep-terms
// (from the bank and rooms.yaml) and approved translations into dst.
func (c *Context) Glossary(src, dst string) translate.Glossary {
	var g translate.Glossary
	if c == nil {
		return g
	}
	for _, e := range c.Version.Entries {
		if !applies(e, src) {
			continue
		}
		if t := e.Translations[dst]; t != "" {
			g.Terms = append(g.Terms, translate.Term{Source: e.Spellings(), Target: t})
		} else if e.Keep {
			g.Keep = append(g.Keep, e.Spellings()...)
		}
	}
	for _, s := range c.Base {
		if !slices.Contains(g.Keep, s) {
			g.Keep = append(g.Keep, s)
		}
	}
	return g
}
