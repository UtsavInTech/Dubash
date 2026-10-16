// Copyright 2026 the Dubash contributors. Apache-2.0.

package asr

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSentenceEndsDanda(t *testing.T) {
	text := "आज कार्यक्रम 10 बजे शुरू होगा। मुख्य वक्ता डॉ. अनन्या राव हैं। धन्यवाद॥ और"
	var got []string
	prev := 0
	for _, end := range sentenceEnds(text) {
		got = append(got, strings.TrimSpace(text[prev:end]))
		prev = end
	}
	want := []string{"आज कार्यक्रम 10 बजे शुरू होगा।", "मुख्य वक्ता डॉ. अनन्या राव हैं।", "धन्यवाद॥"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("split %q, want %q", got, want)
	}
}

func TestAssemblerCommitsHindiSentencesFromInterims(t *testing.T) {
	var a assembler
	a.interim("नमस्ते दोस्तों।")
	evs := a.interim("नमस्ते दोस्तों। आज का कार्यक्रम")
	if f := texts(evs, Final); len(f) != 1 || f[0] != "नमस्ते दोस्तों।" {
		t.Errorf("finals %q", f)
	}
	if got := lastInterim(t, evs); got != "आज का कार्यक्रम" {
		t.Errorf("interim %q", got)
	}
}

// TestRunOnCutCountsCharacters: a long unpunctuated Hindi run is cut by
// characters, the same as English, not three times sooner.
func TestRunOnCutCountsCharacters(t *testing.T) {
	var a assembler
	// 25 words of 4 Devanagari characters plus spaces: about 125
	// characters, 325 bytes. Over the 100-byte budget upstream used, but
	// under maxPending characters, and with no clause boundary: nothing
	// may be cut.
	text := strings.TrimSpace(strings.Repeat("कथा ", 25))
	if n := utf8.RuneCountInString(text); n > maxPending || len(text) <= maxPending {
		t.Fatalf("fixture: %d runes, %d bytes", n, len(text))
	}
	if f := texts(a.interim(text), Final); len(f) != 0 {
		t.Errorf("cut a %d-character run: %q", utf8.RuneCountInString(text), f)
	}
	// Past maxPending characters it is cut at a space, on a rune boundary.
	long := strings.TrimSpace(strings.Repeat("कथा ", 60))
	var b assembler
	f := texts(b.interim(long), Final)
	if len(f) == 0 || !utf8.ValidString(f[0]) || utf8.RuneCountInString(f[0]) > maxPending {
		t.Errorf("long run: %q", f)
	}
}

func TestCommitWords(t *testing.T) {
	var a assembler
	a.interim("सोई ब्रह्मा जी प्रकट हो गए रुकियो रुकियो")
	evs := a.commitWords(4)
	if f := texts(evs, Final); len(f) != 1 || f[0] != "सोई ब्रह्मा जी प्रकट" {
		t.Fatalf("finals %q", f)
	}
	if got := lastInterim(t, evs); got != "हो गए रुकियो रुकियो" {
		t.Errorf("interim %q", got)
	}
	// The next cumulative partial skips the committed words.
	if got := lastInterim(t, a.interim("सोई ब्रह्मा जी प्रकट हो गए रुकियो रुकियो रुकियो")); got != "हो गए रुकियो रुकियो रुकियो" {
		t.Errorf("next interim %q", got)
	}
	// The utterance final, now punctuated, only adds what is left.
	f := texts(a.final("सोई ब्रह्मा जी प्रकट हो गए रुकियो रुकियो रुकियो हनुमान जी बोले।"), Final)
	if len(f) != 1 || f[0] != "हो गए रुकियो रुकियो रुकियो हनुमान जी बोले।" {
		t.Errorf("final %q", f)
	}
}

// TestFinalAlignsRewrittenWords: Sarvam's final often rewrites what its
// partials said; the words committed early must not come back twice.
func TestFinalAlignsRewrittenWords(t *testing.T) {
	cases := []struct{ partial, early, final, want string }{
		// The final writes the number in digits.
		{"आज का कार्यक्रम सुबह दस बजे शुरू", "आज का कार्यक्रम सुबह दस बजे", "आज का कार्यक्रम सुबह 10 बजे शुरू होगा।", "शुरू होगा।"},
		// codemix writes an English word in Latin script.
		{"इस सत्र की रिकॉर्डिंग नहीं", "इस सत्र की रिकॉर्डिंग", "इस सत्र की recording नहीं होगी।", "नहीं होगी।"},
		// Two words merge into one in the final.
		{"हमारी मुख्यवक्ता डॉक्टर अनन्या राव", "हमारी मुख्यवक्ता डॉक्टर", "हमारी मुख्य वक्ता डॉक्टर अनन्या राव हैं।", "अनन्या राव हैं।"},
	}
	for _, c := range cases {
		var a assembler
		a.interim(c.partial)
		a.commitWords(len(strings.Fields(c.early)))
		f := texts(a.final(c.final), Final)
		if len(f) != 1 || f[0] != c.want {
			t.Errorf("final after %q: %q, want %q", c.early, f, c.want)
		}
	}
}
