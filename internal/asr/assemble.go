package asr

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// minWordsAfter is how many words must follow a sentence end in an interim
// before the sentence is trusted: the tail of a hypothesis gets revised
// ("room." becomes "room with"), text followed by more words does not.
const minWordsAfter = 2

// Long sentences are committed in pieces so translated views, which only
// show finals, do not wait on them. Past softPending characters the
// uncommitted text is cut at a clause boundary (", ", "; ", ": ") that
// leaves a piece of at least minClauseWords; past maxPending, a run-on
// speaker is cut at any space. Lengths count characters, not bytes: a
// Devanagari character takes three bytes, and counting bytes cut Hindi
// three times sooner than English.
const (
	softPending    = 100
	minClauseWords = 5
	maxPending     = 200
)

// assembler turns the Live API's transcription into caption events.
//
// The server sends, every half second, the whole text of the utterance
// so far (interims), and closes the utterance with its final text only
// when voice activity detection ends it, which during continuous speech
// takes 20 s or more. So sentences are committed from the interims as
// soon as they are stable, and the utterance final only adds what is left.
//
// What was already committed is found in each new interim and in the final
// by aligning the committed words against the text (see alignRest), not by
// counting words or sentences: recognizers revise their earlier words, and
// a final often splits sentences differently from its interims (Sarvam's
// do), which made counting repeat or drop text.
// An utterance final that ends mid-sentence is carried over and joined to
// the next utterance's first sentence, but it never waits past the end of
// that next utterance.
type assembler struct {
	carry string // unpunctuated end of a closed utterance, not yet emitted
	rest  string // uncommitted text of the latest interim

	// done are the words the current utterance has already committed, in
	// order: sentences, run-on cuts, stable words.
	done []string

	// finalsOnly commits nothing from interims: no sentences, no run-on
	// cuts. For recognizers whose interims are rougher than their finals.
	finalsOnly bool
}

// uncommitted returns what text says after the utterance's committed
// words.
func (a *assembler) uncommitted(text string) string {
	if len(a.done) == 0 {
		return strings.TrimSpace(text)
	}
	return alignRest(text, a.done)
}

// interim takes the cumulative text of the current utterance. It returns
// the sentences that became stable as finals, followed by an interim with
// the text still uncommitted.
func (a *assembler) interim(text string) []Event {
	a.rest = a.uncommitted(text)
	if a.finalsOnly {
		if t := strings.TrimSpace(join(a.carry, a.rest)); t != "" {
			return []Event{{Kind: Interim, Text: t}}
		}
		return nil
	}
	var out []Event
	ends := sentenceEnds(a.rest)
	prev := 0
	for _, end := range ends {
		if len(strings.Fields(a.rest[end:])) < minWordsAfter {
			break
		}
		out = a.commit(out, a.rest[prev:end])
		prev = end
	}
	a.rest = strings.TrimSpace(a.rest[prev:])
	for utf8.RuneCountInString(a.rest) > softPending {
		cut := runOnCut(a.rest, utf8.RuneCountInString(a.rest) > maxPending)
		if cut <= 0 {
			break
		}
		out = a.commit(out, a.rest[:cut])
		a.rest = strings.TrimSpace(a.rest[cut:])
	}
	if t := strings.TrimSpace(join(a.carry, a.rest)); t != "" {
		out = append(out, Event{Kind: Interim, Text: t})
	}
	return out
}

// settle commits the uncommitted text when it ends a sentence. The engine
// calls it once the interims have stopped changing: a speaker who pauses
// after a sentence gets it out now instead of when the server closes the
// utterance, which can take many seconds.
func (a *assembler) settle() []Event {
	ends := sentenceEnds(a.rest)
	if len(ends) == 0 || ends[len(ends)-1] != len(a.rest) {
		return nil
	}
	var out []Event
	prev := 0
	for _, end := range ends {
		out = a.commit(out, a.rest[prev:end])
		prev = end
	}
	a.rest = ""
	return out
}

// final takes the text of a closed utterance and returns finals for what
// its interims did not already commit.
func (a *assembler) final(text string) []Event {
	rest := a.uncommitted(text)
	stale := a.carry != "" // already waited for one utterance
	var out []Event
	defer a.resetUtterance()
	prev := 0
	for _, end := range sentenceEnds(rest) {
		out = a.commit(out, rest[prev:end])
		prev = end
	}
	tail := strings.TrimSpace(rest[prev:])
	if stale && a.carry != "" || utf8.RuneCountInString(tail) > maxPending {
		return a.commit(out, tail)
	}
	a.carry = tail
	return out
}

// endSession closes the current utterance when its session ends without
// an utterance final: the uncommitted interim text is carried over, and
// the next session's interims start from scratch.
func (a *assembler) endSession() {
	a.carry = strings.TrimSpace(join(a.carry, a.rest))
	a.resetUtterance()
}

// commitWords commits the first n words of the uncommitted text as a
// final, for recognizers whose interims carry no punctuation (Sarvam's
// partial transcripts): the engine decides which words are stable enough.
// It returns the final followed by an interim with the rest.
func (a *assembler) commitWords(n int) []Event {
	words := strings.Fields(a.rest)
	if n <= 0 || len(words) == 0 {
		return nil
	}
	n = min(n, len(words))
	out := a.commit(nil, strings.Join(words[:n], " "))
	a.rest = strings.Join(words[n:], " ")
	if t := strings.TrimSpace(join(a.carry, a.rest)); t != "" {
		out = append(out, Event{Kind: Interim, Text: t})
	}
	return out
}

// flush emits everything still pending as a final.
func (a *assembler) flush() []Event {
	a.endSession()
	return a.commit(nil, "")
}

func (a *assembler) resetUtterance() {
	a.rest = ""
	a.done = nil
}

// commit appends text, preceded by any carried text, as a final.
func (a *assembler) commit(out []Event, text string) []Event {
	a.done = append(a.done, strings.Fields(text)...)
	t := strings.TrimSpace(join(a.carry, strings.TrimSpace(text)))
	a.carry = ""
	if t == "" {
		return out
	}
	return append(out, Event{Kind: Final, Text: t})
}

// abbreviations end in a period that does not end a sentence.
var abbreviations = map[string]bool{
	"e.g.": true, "i.e.": true, "etc.": true, "vs.": true, "dr.": true, "dra.": true,
	"mr.": true, "mrs.": true, "ms.": true, "sr.": true, "sra.": true, "prof.": true,
	"ej.": true, "p.ej.": true, "ud.": true, "uds.": true,
	"डॉ.": true, "श्री.": true, "सं.": true,
}

// isTerminal ends a sentence: Latin punctuation, and the danda and double
// danda of Hindi, Marathi, Bengali, Odia and Punjabi text.
func isTerminal(r rune) bool { return strings.ContainsRune(".?!…।॥", r) }

func isCloser(r rune) bool { return strings.ContainsRune(`"')]»”’`, r) }

// sentenceEnds returns the byte offsets just past each complete sentence
// in text: a run of terminal punctuation (and closing quotes) followed by
// whitespace or the end of the text. Numbers such as 3.5 and common
// abbreviations do not end sentences.
func sentenceEnds(text string) []int {
	var ends []int
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		if !isTerminal(r) {
			i += n
			continue
		}
		start := i
		j := i + n
		for j < len(text) {
			r2, n2 := utf8.DecodeRuneInString(text[j:])
			if !isTerminal(r2) && !isCloser(r2) {
				break
			}
			j += n2
		}
		i = j
		if j < len(text) {
			if r2, _ := utf8.DecodeRuneInString(text[j:]); !unicode.IsSpace(r2) {
				continue
			}
		}
		if r == '.' && j-start == 1 {
			word := text[strings.LastIndexFunc(text[:start], unicode.IsSpace)+1 : j]
			if abbreviations[strings.ToLower(word)] {
				continue
			}
		}
		ends = append(ends, j)
	}
	return ends
}

// runOnCut returns where to cut a long text: after the last clause
// boundary (", ", "; ", ": ") that keeps minClauseWords before it and
// minWordsAfter words after it, or, when anywhere is allowed, at the last
// space within maxPending bytes. It returns 0 if there is nowhere to cut.
func runOnCut(text string, anywhere bool) int {
	best := 0
	for i := 0; i+1 < len(text); i++ {
		if strings.IndexByte(",;:", text[i]) >= 0 && text[i+1] == ' ' &&
			len(strings.Fields(text[:i+1])) >= minClauseWords &&
			len(strings.Fields(text[i+1:])) >= minWordsAfter {
			best = i + 1
		}
	}
	if best > 0 {
		return best
	}
	// No clause punctuation: cut at the last phrase end within maxPending
	// characters (after a clause-final verb, before a conjunction), never
	// mid-phrase. Only a speaker who runs on for twice that without one is
	// cut at a space.
	head := text[:runeOffset(text, maxPending)]
	words := strings.Fields(text)
	if n := lastPhraseEnd(words, minClauseWords, len(strings.Fields(head))); n > 0 && len(words)-n >= minWordsAfter {
		return len(strings.Join(words[:n], " "))
	}
	if !anywhere || utf8.RuneCountInString(text) <= 2*maxPending {
		return 0
	}
	return strings.LastIndexByte(head, ' ')
}

// runeOffset is the byte offset of the n-th rune of text, or len(text).
func runeOffset(text string, n int) int {
	for i := range text {
		if n == 0 {
			return i
		}
		n--
	}
	return len(text)
}

// alignRest returns what text says after the words already committed.
// The cut is where text's leading words best match committed, searching a
// few words either side of their count: first by characters with spaces
// and punctuation ignored (so "मुख्यवक्ता" matches "मुख्य वक्ता"), then by
// words, then nearest to the committed count.
func alignRest(text string, committed []string) string {
	words := strings.Fields(text)
	key := func(w string) string {
		return strings.ToLower(strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) || isTerminal(r) }))
	}
	want := make([]string, len(committed))
	for i, w := range committed {
		want[i] = key(w)
	}
	have := make([]string, len(words))
	for i, w := range words {
		have[i] = key(w)
	}
	wantChars := []rune(strings.Join(want, ""))
	abs := func(n int) int { return max(n, -n) }
	type score struct{ chars, words, off int }
	less := func(x, y score) bool {
		if x.chars != y.chars {
			return x.chars < y.chars
		}
		if x.words != y.words {
			return x.words < y.words
		}
		return x.off < y.off
	}
	// Recognizers add and drop words as they revise: allow more drift on
	// a longer committed text.
	slack := max(4, len(committed)/5)
	best, bestScore := -1, score{}
	for k := max(0, len(committed)-slack); k <= min(len(words), len(committed)+slack); k++ {
		sc := score{
			chars: editDistance(wantChars, []rune(strings.Join(have[:k], ""))),
			words: editDistance(want, have[:k]),
			off:   abs(k - len(committed)),
		}
		if best < 0 || less(sc, bestScore) {
			best, bestScore = k, sc
		}
	}
	if best < 0 {
		return ""
	}
	return strings.Join(words[best:], " ")
}

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

// join concatenates transcript chunks, adding a space only when neither
// side already has one and the second chunk doesn't start with punctuation.
func join(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	last, _ := utf8.DecodeLastRuneInString(a)
	first, _ := utf8.DecodeRuneInString(b)
	if unicode.IsSpace(last) || unicode.IsSpace(first) || unicode.IsPunct(first) {
		return a + b
	}
	return a + " " + b
}
