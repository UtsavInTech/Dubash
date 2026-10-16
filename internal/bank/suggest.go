// Copyright 2026 the Dubash contributors. Apache-2.0.

package bank

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Limits on what a live fix may suggest: short spans only, so a rewritten
// sentence is not mistaken for vocabulary.
const (
	maxSuggestWords  = 4
	maxMisheardWords = 6
)

// Suggest compares a recognized line with its correction, word by word,
// and returns a suggested event entry for each short span replaced: the
// corrected words as the term, the recognized ones as a mis-hearing.
// Changes to numbers, case or punctuation alone suggest nothing: they are
// not vocabulary.
func Suggest(raw, corrected, lang, event string) []Entry {
	a, b := words(raw), words(corrected)
	ka, kb := keys(a), keys(b)
	var out []Entry
	for _, op := range diff(ka, kb) {
		ra, rb := a[op.a0:op.a1], b[op.b0:op.b1]
		if len(ra) == 0 || len(rb) == 0 || len(rb) > maxSuggestWords || len(ra) > maxMisheardWords {
			continue
		}
		term, heard := strings.Join(rb, " "), strings.Join(ra, " ")
		if Fold(term) == Fold(heard) || numeric(term) || numeric(heard) {
			continue
		}
		out = append(out, Entry{
			Scope: ScopeEvent, ScopeID: event, Term: term, Lang: lang, Misheard: []string{heard},
			Status: Suggested, Source: SourceCorrection,
		})
	}
	return out
}

// words splits text into words without the punctuation around them.
func words(s string) []string {
	var out []string
	for _, w := range strings.Fields(s) {
		w = strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) || r == '।' || r == '॥' })
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

func keys(ws []string) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = Fold(w)
	}
	return out
}

func numeric(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool {
		return !unicode.IsDigit(r) && !unicode.IsSpace(r) && !strings.ContainsRune(".,:-/%₹", r)
	}) < 0
}

// op is a changed span: a[a0:a1] became b[b0:b1].
type op struct{ a0, a1, b0, b1 int }

// diff returns the spans where a and b differ, from a longest common
// subsequence of words.
func diff(a, b []string) []op {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []op
	i, j := 0, 0
	start := func() op { return op{a0: i, b0: j} }
	var cur *op
	flush := func() {
		if cur != nil {
			cur.a1, cur.b1 = i, j
			out = append(out, *cur)
			cur = nil
		}
	}
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			flush()
			i++
			j++
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			if cur == nil {
				c := start()
				cur = &c
			}
			j++
		default:
			if cur == nil {
				c := start()
				cur = &c
			}
			i++
		}
	}
	flush()
	return out
}

// SuggestFrom stores the suggestions of a correction, skipping any whose
// term and mis-hearing the event's entries (of any status but rejected)
// already have, and returns the ids stored.
func SuggestFrom(ctx context.Context, s Store, c Correction, actor string) ([]string, error) {
	existing, err := s.Entries(ctx, Filter{Scope: ScopeEvent, ScopeID: c.Event})
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, sug := range Suggest(c.Raw, c.Corrected, c.Lang, c.Event) {
		known := slices.ContainsFunc(existing, func(e Entry) bool {
			return e.Status != Rejected && Fold(e.Term) == Fold(sug.Term) &&
				slices.ContainsFunc(e.Misheard, func(m string) bool { return Fold(m) == Fold(sug.Misheard[0]) })
		})
		if known {
			continue
		}
		sug.Note = fmt.Sprintf("from a live fix of line %d", c.Segment)
		saved, err := s.SaveEntry(ctx, sug, actor)
		if err != nil {
			return ids, err
		}
		existing = append(existing, saved)
		ids = append(ids, saved.ID)
	}
	return ids, nil
}

// Approve approves an entry. When an approved entry with the same term
// already exists in the same scope, the suggestion is merged into it (its
// spellings and mis-hearings added, as a new version) and retired, so the
// bank does not fill with duplicates. It returns the approved entry.
func Approve(ctx context.Context, s Store, id, actor string) (Entry, error) {
	e, err := s.Entry(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	peers, err := s.Entries(ctx, Filter{Scope: e.Scope, ScopeID: e.ScopeID, Status: Approved})
	if err != nil {
		return Entry{}, err
	}
	for _, p := range peers {
		if p.ID == e.ID || Fold(p.Term) != Fold(e.Term) || p.Lang != e.Lang {
			continue
		}
		p.Aliases = append(p.Aliases, e.Aliases...)
		p.Misheard = append(p.Misheard, e.Misheard...)
		for l, t := range e.Translations {
			if p.Translations == nil {
				p.Translations = map[string]string{}
			}
			if p.Translations[l] == "" {
				p.Translations[l] = t
			}
		}
		merged, err := s.SaveEntry(ctx, p, actor)
		if err != nil {
			return Entry{}, err
		}
		e.Status, e.Note = Retired, fmt.Sprintf("merged into %s", p.ID)
		if _, err := s.SaveEntry(ctx, e, actor); err != nil {
			return Entry{}, err
		}
		return merged, nil
	}
	e.Status = Approved
	return s.SaveEntry(ctx, e, actor)
}
