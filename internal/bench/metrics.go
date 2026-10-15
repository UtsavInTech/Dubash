// Copyright 2026 the Dubash contributors. Apache-2.0.

package bench

import (
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// indicZeros are the code points of digit zero in the Indic scripts, so
// "२०२६" and "2026" compare equal.
var indicZeros = []rune{0x0966, 0x09E6, 0x0A66, 0x0AE6, 0x0B66, 0x0BE6, 0x0C66, 0x0CE6, 0x0D66}

func asciiDigit(r rune) (rune, bool) {
	for _, z := range indicZeros {
		if r >= z && r <= z+9 {
			return '0' + (r - z), true
		}
	}
	return r, false
}

// nukta is the Devanagari nukta. Writers and recognizers use it
// inconsistently ("फ़ोन" and "फोन" are the same word to a reader), so
// scoring ignores it.
const nukta = '\u093C'

// Normalize prepares text for scoring: NFC, lower case, nukta dropped, Indic digits as
// ASCII, digit group separators removed ("1,00,000" → "100000"),
// apostrophes dropped ("don't" → "dont") and every other punctuation mark,
// including the danda, turned into a space. Letters and combining marks
// (the vowel signs of Indic scripts) are kept.
func Normalize(s string) string {
	// Decompose first so precomposed nukta letters (U+0958–U+095F) lose it too.
	s = strings.ReplaceAll(norm.NFD.String(strings.ToLower(s)), string(nukta), "")
	rs := []rune(norm.NFC.String(s))
	var b strings.Builder
	for i, r := range rs {
		if d, ok := asciiDigit(r); ok {
			r = d
		}
		switch {
		case r == '\'' || r == '’' || r == 'ʼ':
			continue
		case (r == ',' || r == '.') && i > 0 && i+1 < len(rs) && isDigit(rs[i-1]) && isDigit(rs[i+1]):
			if r == ',' {
				continue
			}
			b.WriteRune(r) // a decimal point stays
		case unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func isDigit(r rune) bool {
	_, ok := asciiDigit(r)
	return ok || r >= '0' && r <= '9'
}

// Tokens are the words of the normalized text.
func Tokens(s string) []string { return strings.Fields(Normalize(s)) }

// editDistance is the Levenshtein distance between two sequences.
func editDistance[T comparable](a, b []T) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// WER is the word error rate of hyp against ref (0 is perfect; it can
// exceed 1 when hyp inserts many words).
func WER(ref, hyp string) float64 {
	r, h := Tokens(ref), Tokens(hyp)
	if len(r) == 0 {
		return float64(len(h))
	}
	return float64(editDistance(r, h)) / float64(len(r))
}

// CER is the character error rate, more telling than WER for Telugu and
// Kannada, whose long agglutinated words turn one wrong suffix into a
// whole wrong word.
func CER(ref, hyp string) float64 {
	r, h := []rune(Normalize(ref)), []rune(Normalize(hyp))
	if len(r) == 0 {
		return float64(len(h))
	}
	return float64(editDistance(r, h)) / float64(len(r))
}

// count is how many times the token sequence term occurs in toks.
func count(toks, term []string) int {
	if len(term) == 0 {
		return 0
	}
	n := 0
	for i := 0; i+len(term) <= len(toks); i++ {
		match := true
		for j, t := range term {
			if toks[i+j] != t {
				match = false
				break
			}
		}
		if match {
			n++
		}
	}
	return n
}

// countAny counts non-overlapping occurrences of any of the spellings in
// toks, trying the longest spelling first at each position, so an alias
// that contains the term ("Sarvam AI" and "Sarvam") counts once.
func countAny(toks []string, spellings []string) int {
	var seqs [][]string
	for _, sp := range spellings {
		if t := Tokens(sp); len(t) > 0 {
			seqs = append(seqs, t)
		}
	}
	slices.SortStableFunc(seqs, func(a, b []string) int { return len(b) - len(a) })
	n := 0
	for i := 0; i < len(toks); {
		matched := 0
		for _, seq := range seqs {
			if i+len(seq) <= len(toks) && slices.Equal(toks[i:i+len(seq)], seq) {
				matched = len(seq)
				break
			}
		}
		if matched > 0 {
			n++
			i += matched
		} else {
			i++
		}
	}
	return n
}

// TermScore compares how often each vocabulary term is spoken (in ref)
// with how often it is recognized (in hyp).
type TermScore struct {
	Expected int `json:"expected"` // occurrences in the reference
	Found    int `json:"found"`    // of those, recognized
	Inserted int `json:"inserted"` // recognized more often than spoken: a wrongly inserted term
	// Missed lists terms spoken but not recognized.
	Missed []string `json:"missed,omitempty"`
	// Spurious lists terms recognized but never spoken.
	Spurious []string `json:"spurious,omitempty"`
}

// Recall is Found/Expected, or 1 with nothing expected.
func (t TermScore) Recall() float64 {
	if t.Expected == 0 {
		return 1
	}
	return float64(t.Found) / float64(t.Expected)
}

// ScoreTerms counts terms in ref and hyp. Each term may have spellings
// (the canonical form first) that all count as that term.
func ScoreTerms(ref, hyp string, terms [][]string) TermScore {
	rt, ht := Tokens(ref), Tokens(hyp)
	var s TermScore
	for _, spellings := range terms {
		inRef, inHyp := countAny(rt, spellings), countAny(ht, spellings)
		s.Expected += inRef
		s.Found += min(inRef, inHyp)
		if inHyp > inRef {
			s.Inserted += inHyp - inRef
			if inRef == 0 {
				s.Spurious = append(s.Spurious, spellings[0])
			}
		}
		if inHyp < inRef {
			s.Missed = append(s.Missed, spellings[0])
		}
	}
	return s
}

// NumberScore compares the numbers (digit sequences) of ref and hyp as
// multisets. Numbers spoken as words in the reference ("twenty",
// "बीस") are not counted: write references with digits where the
// speaker says a figure.
type NumberScore struct {
	Expected int      `json:"expected"`
	Correct  int      `json:"correct"`
	Wrong    []string `json:"wrong,omitempty"` // in hyp but not in ref
}

// ScoreNumbers counts the reference's numbers that hyp reproduces.
func ScoreNumbers(ref, hyp string) NumberScore {
	nums := func(s string) map[string]int {
		m := map[string]int{}
		for _, t := range Tokens(s) {
			if strings.IndexFunc(t, func(r rune) bool { return !(r >= '0' && r <= '9' || r == '.') }) < 0 && strings.ContainsAny(t, "0123456789") {
				m[t]++
			}
		}
		return m
	}
	r, h := nums(ref), nums(hyp)
	var s NumberScore
	for n, c := range r {
		s.Expected += c
		s.Correct += min(c, h[n])
	}
	for n, c := range h {
		if c > r[n] {
			s.Wrong = append(s.Wrong, n)
		}
	}
	return s
}

// negations are the standalone negation words per language. Telugu,
// Kannada and Tamil also negate with verb suffixes, which a word list
// cannot see; their counts are a lower bound.
var negations = map[string][]string{
	"en": {"not", "no", "never", "nothing", "none", "nobody", "neither", "nor", "cannot", "without",
		"dont", "doesnt", "didnt", "wont", "cant", "isnt", "arent", "wasnt", "werent", "shouldnt", "wouldnt", "couldnt", "havent", "hasnt"},
	"hi": {"नहीं", "नही", "न", "ना", "मत", "बिना", "कोई नहीं"},
	"te": {"కాదు", "లేదు", "వద్దు", "రాదు", "లేని"},
	"kn": {"ಇಲ್ಲ", "ಅಲ್ಲ", "ಬೇಡ", "ಇಲ್ಲದ"},
	"ta": {"இல்லை", "அல்ல", "வேண்டாம்", "இல்லாத"},
}

// NegationScore counts negation words in ref and hyp. A difference means
// a negation was lost or invented, the most harmful error in a caption.
type NegationScore struct {
	Expected int `json:"expected"`
	Found    int `json:"found"`
}

// Agrees reports whether hyp has exactly as many negations as ref.
func (n NegationScore) Agrees() bool { return n.Expected == n.Found }

// ScoreNegations counts negation words of lang (and of English, since
// Indian speakers code-switch).
func ScoreNegations(ref, hyp, lang string) NegationScore {
	words := negations[lang]
	if lang != "en" {
		words = append(append([]string(nil), words...), negations["en"]...)
	}
	rt, ht := Tokens(ref), Tokens(hyp)
	var s NegationScore
	for _, w := range words {
		s.Expected += count(rt, Tokens(w))
		s.Found += count(ht, Tokens(w))
	}
	return s
}

// ChrF is the chrF2 score (character 1- to 6-gram F-score, recall weighted
// twice) of hyp against ref, from 0 to 100. It suits translation into
// Indian languages better than BLEU, which punishes legitimate variation
// in word segmentation.
func ChrF(ref, hyp string) float64 {
	const maxN, beta = 6, 2.0
	r := []rune(strings.ReplaceAll(Normalize(ref), " ", ""))
	h := []rune(strings.ReplaceAll(Normalize(hyp), " ", ""))
	var precSum, recSum float64
	used := 0
	for n := 1; n <= maxN; n++ {
		rg, hg := ngrams(r, n), ngrams(h, n)
		var rTotal, hTotal, match int
		for _, c := range rg {
			rTotal += c
		}
		for g, c := range hg {
			hTotal += c
			match += min(c, rg[g])
		}
		if rTotal == 0 || hTotal == 0 {
			continue
		}
		precSum += float64(match) / float64(hTotal)
		recSum += float64(match) / float64(rTotal)
		used++
	}
	if used == 0 {
		return 0
	}
	p, rc := precSum/float64(used), recSum/float64(used)
	if p+rc == 0 {
		return 0
	}
	return 100 * (1 + beta*beta) * p * rc / (beta*beta*p + rc)
}

func ngrams(r []rune, n int) map[string]int {
	m := map[string]int{}
	for i := 0; i+n <= len(r); i++ {
		m[string(r[i:i+n])]++
	}
	return m
}
