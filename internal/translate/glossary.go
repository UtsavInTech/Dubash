// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"fmt"
	"strings"
)

// Glossary is what a translation into one language must respect, from
// the event's pinned Dictation Bank version (and rooms.yaml's glossary).
type Glossary struct {
	// Keep are terms shown as written in every language: never
	// translated, never transliterated.
	Keep []string
	// Terms have an approved translation into the target language.
	Terms []Term
}

// Term is a source term, said in any of its spellings, and the approved
// rendering of it in the target language.
type Term struct {
	Source []string // the canonical spelling first, then aliases
	Target string
}

// Keep returns a glossary of terms never to translate.
func Keep(terms ...string) Glossary { return Glossary{Keep: terms} }

// Empty reports whether g asks for nothing.
func (g Glossary) Empty() bool { return len(g.Keep) == 0 && len(g.Terms) == 0 }

// slot is a span replaced by a placeholder: the spellings it covers and
// the text to put back.
type slot struct {
	spellings []string
	restore   string
}

// slots lists what Mask replaces: every keep-term (restored as written)
// and every spelling of an approved term (restored as its translation).
func (g Glossary) slots() []slot {
	var out []slot
	for _, k := range g.Keep {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, slot{spellings: []string{k}, restore: k})
		}
	}
	for _, t := range g.Terms {
		if strings.TrimSpace(t.Target) == "" {
			continue
		}
		var sp []string
		for _, s := range t.Source {
			if s = strings.TrimSpace(s); s != "" {
				sp = append(sp, s)
			}
		}
		if len(sp) > 0 {
			out = append(out, slot{spellings: sp, restore: t.Target})
		}
	}
	return out
}

// promptRules phrases g for a language model's system instruction.
func (g Glossary) promptRules(dst string) string {
	var b strings.Builder
	if len(g.Keep) > 0 {
		fmt.Fprintf(&b, "\nNever translate or transliterate these terms; keep them exactly as written: %s.", strings.Join(g.Keep, ", "))
	}
	if len(g.Terms) > 0 {
		fmt.Fprintf(&b, "\nUse these approved translations into %s:", name(dst))
		for _, t := range g.Terms {
			fmt.Fprintf(&b, "\n- %s → %s", strings.Join(t.Source, " / "), t.Target)
		}
	}
	return b.String()
}
