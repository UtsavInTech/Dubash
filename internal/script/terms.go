// Copyright 2026 the Dubash contributors. Apache-2.0.

package script

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Candidate is a term found in a script that may belong in the Dictation
// Bank: a name, a brand, an English word inside Indian-language text, a
// rare word repeated.
type Candidate struct {
	Term  string `json:"term"`
	Count int    `json:"count"`
	Why   string `json:"why"`
}

// maxCandidates bounds the suggestions from one script.
const maxCandidates = 40

// common are frequent words that are never vocabulary.
var common = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`the a an and or but of to in on at for with from by is are was were be been
	this that these those it its as we you they he she i our your their my me us them not no yes all any
	can will would should could may might must have has had do does did so if then than when what which who
	और का के की को से में है हैं था थे थी हो भी तो कि यह ये वह वो एक हम आप तुम मैं नहीं पर लिए साथ अब
	जब तक कर करते करना किया गया गए रहा रहे रही सकते होता होती होते सब कुछ बहुत इस उस जो क्या क्यों कैसे
	हमारे हमारी आपके आपकी उनके उनकी अपने अपनी इसके उसके बात बातें लोग समय दिन`) {
		m[w] = true
	}
	return m
}()

func isLatin(w string) bool {
	return strings.IndexFunc(w, func(r rune) bool { return unicode.IsLetter(r) && r > unicode.MaxLatin1 }) < 0 &&
		strings.IndexFunc(w, unicode.IsLetter) >= 0
}

func hasDigit(w string) bool { return strings.IndexFunc(w, unicode.IsDigit) >= 0 }

// SuggestTerms lists likely vocabulary in a script, most frequent first:
// runs of capitalized Latin words (names, brands), Latin words in
// Indian-language text (code-mixed terms), and rare Indian-language words
// said at least twice. Numbers are not vocabulary.
func SuggestTerms(text string) []Candidate {
	counts := map[string]*Candidate{}
	add := func(term, why string) {
		term = strings.TrimSpace(term)
		if term == "" || hasDigit(term) || common[Fold(term)] {
			return
		}
		k := Fold(term)
		if c, ok := counts[k]; ok {
			c.Count++
			return
		}
		counts[k] = &Candidate{Term: term, Count: 1, Why: why}
	}
	indic := strings.IndexFunc(text, func(r rune) bool { return r >= 0x0900 && r < 0x0D80 }) >= 0
	for _, para := range strings.Split(text, "\n\n") {
		var words []string
		var ends []bool // the raw word ended a sentence
		for _, w := range strings.Fields(para) {
			last, _ := utf8.DecodeLastRuneInString(w)
			ends = append(ends, strings.ContainsRune(".?!।॥", last) && !strings.EqualFold(w, "dr.") && !strings.EqualFold(w, "mr.") && !strings.EqualFold(w, "ms."))
			words = append(words, strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) || r == '।' || r == '॥' }))
		}
		for i := 0; i < len(words); i++ {
			w := words[i]
			if w == "" {
				continue
			}
			sentenceStart := i == 0 || ends[i-1]
			r, _ := utf8.DecodeRuneInString(w)
			switch {
			case isLatin(w) && unicode.IsUpper(r):
				// A run of capitalized words: "Sarvam AI", "Ananya Rao",
				// within one sentence.
				j := i + 1
				for j < len(words) && j-i < 4 && !ends[j-1] && words[j] != "" && isLatin(words[j]) {
					r2, _ := utf8.DecodeRuneInString(words[j])
					if !unicode.IsUpper(r2) {
						break
					}
					j++
				}
				// A lone capital opening an English sentence is not a name.
				if j-i > 1 || !sentenceStart || indic {
					add(strings.Join(words[i:j], " "), "name")
				}
				i = j - 1
			case isLatin(w) && indic && utf8.RuneCountInString(w) >= 3:
				add(w, "English in Indian-language text")
			case !isLatin(w) && utf8.RuneCountInString(w) >= 5:
				add(w, "repeated word")
			}
		}
	}
	var out []Candidate
	for _, c := range counts {
		if c.Why == "repeated word" && c.Count < 2 {
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Term < out[j].Term
	})
	if len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	return out
}
