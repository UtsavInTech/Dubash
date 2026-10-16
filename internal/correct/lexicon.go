// Copyright 2026 the Dubash contributors. Apache-2.0.

package correct

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/UtsavInTech/dubash/internal/script"
)

// Lexicon is a list of real words per language. A real word is not a
// near miss: "ब्रह्म" (Brahman) must never be "corrected" into the bank
// term "ब्रह्मा" (Brahma) because it is one letter away. Only the
// near-miss rule consults it; a bank entry that lists a word as a known
// mis-hearing still applies, and the model, which sees the context, is
// held by its own rules.
type Lexicon map[string]map[string]bool

// Has reports whether word is a real word of lang.
func (l Lexicon) Has(lang, word string) bool {
	return l[lang][script.Fold(word)]
}

// LoadLexicon reads word lists in JSON Lines, one word per line with
// "term" and "language" fields (the format of the Indian discourse
// vocabulary bank's vocabulary.jsonl). Several files may be given.
func LoadLexicon(paths ...string) (Lexicon, error) {
	l := Lexicon{}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		n := 0
		for sc.Scan() {
			var r struct {
				Term     string `json:"term"`
				Language string `json:"language"`
			}
			if json.Unmarshal(sc.Bytes(), &r) != nil || r.Term == "" || r.Language == "" {
				continue
			}
			if l[r.Language] == nil {
				l[r.Language] = map[string]bool{}
			}
			l[r.Language][script.Fold(strings.TrimSpace(r.Term))] = true
			n++
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("lexicon %s: %w", p, err)
		}
		if n == 0 {
			return nil, fmt.Errorf("lexicon %s: no words (want JSON lines with term and language)", p)
		}
	}
	return l, nil
}

// realWords reports whether every word of span is in the lexicon.
func (l Lexicon) realWords(lang, span string) bool {
	ws := words(span)
	if len(l[lang]) == 0 || len(ws) == 0 {
		return false
	}
	for _, w := range ws {
		if !l.Has(lang, w) {
			return false
		}
	}
	return true
}
