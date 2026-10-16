// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package correct fixes recognition errors in a committed caption line,
// using the event's Dictation Bank and script, before the line is
// published and translated. It is bounded: it makes small edits only, each
// backed by evidence and checked by the same rules (numbers, negations
// and meaning untouched, nothing inserted that was not said), and it has
// a deadline, after which the recognized line goes out as it was.
package correct

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/UtsavInTech/dubash/internal/bank"
	"github.com/UtsavInTech/dubash/internal/script"
)

// Edit is one replacement made in a line.
type Edit struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Rule  string `json:"rule"`            // misheard, fuzzy or model
	Entry string `json:"entry,omitempty"` // the bank entry behind it
}

// Result is a corrected line.
type Result struct {
	Text  string `json:"text"`
	Raw   string `json:"raw"`
	Edits []Edit `json:"edits,omitempty"`
	// Skipped are edits the model proposed that the rules refused.
	Skipped  []Edit        `json:"skipped,omitempty"`
	TimedOut bool          `json:"timedOut,omitempty"`
	Took     time.Duration `json:"took"`
}

// Input is a line and its evidence.
type Input struct {
	Text string
	Lang string
	// Entries are the approved bank entries in force for the event.
	Entries []bank.Entry
	// Script is the passage the line is probably from, and the aligner's
	// confidence in it (0: do not use it).
	Script     string
	ScriptConf float64
	// Previous are the last committed lines, oldest first.
	Previous []string
}

// minScriptConf is the confidence the script needs to count as evidence.
const minScriptConf = 0.4

// Model proposes edits for a line; LLM implements it.
type Model interface {
	Propose(ctx context.Context, in Input, entries []bank.Entry) ([]Edit, error)
}

// Corrector corrects lines.
type Corrector struct {
	// Model, if set, is asked for further edits when there is evidence to
	// use (bank entries or a confident script passage).
	Model Model
	// Deadline bounds the model (default 800 ms); the deterministic rules
	// always run.
	Deadline time.Duration
}

// maxEdits bounds the edits in one line.
const maxEdits = 4

// Correct returns the corrected line. It never fails: whatever cannot be
// done in time, or safely, is left as recognized.
func (c *Corrector) Correct(ctx context.Context, in Input) Result {
	start := time.Now()
	res := Result{Text: in.Text, Raw: in.Text}
	scriptOK := in.Script != "" && in.ScriptConf >= minScriptConf
	evidence := func(e bank.Entry) bool {
		return !e.Ambiguous || scriptOK && containsWords(in.Script, e.Term)
	}
	apply := func(e Edit) bool {
		if len(res.Edits) >= maxEdits {
			return false
		}
		next, ok := Apply(res.Text, e)
		if !ok {
			return false
		}
		res.Text = next
		res.Edits = append(res.Edits, e)
		return true
	}

	// 1. Known mis-hearings, from the bank.
	for _, e := range in.Entries {
		if !applies(e, in.Lang) || !evidence(e) {
			continue
		}
		for _, m := range e.Misheard {
			if containsWords(res.Text, m) {
				apply(Edit{From: m, To: e.Term, Rule: "misheard", Entry: e.ID})
			}
		}
	}

	// 2. Near misses of bank terms, in the same script, within one or two
	//    letters: "बागेश्वर धान" for "बागेश्वर धाम".
	for _, e := range in.Entries {
		if !applies(e, in.Lang) || !evidence(e) || e.Ambiguous {
			continue
		}
		for _, sp := range e.Spellings() {
			if containsWords(res.Text, sp) {
				continue
			}
			if from := nearSpan(res.Text, sp); from != "" {
				apply(Edit{From: from, To: e.Term, Rule: "fuzzy", Entry: e.ID})
				break
			}
		}
	}

	// 3. The model, within the deadline.
	if c != nil && c.Model != nil && (len(in.Entries) > 0 || scriptOK) {
		deadline := c.Deadline
		if deadline <= 0 {
			deadline = 800 * time.Millisecond
		}
		mctx, cancel := context.WithTimeout(ctx, deadline)
		edits, err := c.Model.Propose(mctx, Input{Text: res.Text, Lang: in.Lang, Script: ifConf(in.Script, scriptOK), ScriptConf: in.ScriptConf, Previous: in.Previous}, relevant(res.Text, in.Entries))
		cancel()
		if mctx.Err() != nil {
			res.TimedOut = true
		}
		if err == nil {
			allowed := vocabulary(in.Entries, ifConf(in.Script, scriptOK))
			for _, e := range edits {
				e.Rule = "model"
				if justified(e, allowed) && apply(e) {
					continue
				}
				res.Skipped = append(res.Skipped, e)
			}
		}
	}
	res.Took = time.Since(start)
	return res
}

func ifConf(s string, ok bool) string {
	if ok {
		return s
	}
	return ""
}

func applies(e bank.Entry, lang string) bool {
	return e.Status == bank.Approved && (e.Lang == "" || e.Lang == lang)
}

// ---------- the rules every edit must pass ----------

// negations are words whose number must not change: a lost or invented
// "not" reverses a sentence.
var negations = map[string]bool{
	"नहीं": true, "नही": true, "न": true, "ना": true, "मत": true, "बिना": true,
	"not": true, "no": true, "never": true, "nothing": true, "none": true, "nobody": true, "cannot": true,
	"don't": true, "doesn't": true, "didn't": true, "won't": true, "can't": true, "isn't": true, "aren't": true,
	"wasn't": true, "weren't": true, "shouldn't": true, "wouldn't": true, "couldn't": true,
	"కాదు": true, "లేదు": true, "వద్దు": true, "ಇಲ್ಲ": true, "ಅಲ್ಲ": true, "ಬೇಡ": true, "இல்லை": true, "அல்ல": true,
}

func trimWord(w string) string {
	return strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) || r == '।' || r == '॥' })
}

func words(s string) []string {
	var out []string
	for _, w := range strings.Fields(s) {
		if w = trimWord(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

func countNegations(s string) int {
	n := 0
	for _, w := range words(s) {
		if negations[strings.ToLower(w)] {
			n++
		}
	}
	return n
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Apply makes one edit in text, if it passes the rules: From occurs as
// whole words; numbers and negations are the same before and after; the
// replacement is no longer than the words it replaces plus three.
func Apply(text string, e Edit) (string, bool) {
	from, to := strings.TrimSpace(e.From), strings.TrimSpace(e.To)
	if from == "" || to == "" || from == to {
		return text, false
	}
	if digits(from) != digits(to) || countNegations(from) != countNegations(to) {
		return text, false
	}
	if len(words(to)) > len(words(from))+3 {
		return text, false
	}
	i, n := findWords(text, from)
	if i < 0 {
		return text, false
	}
	out := text[:i] + to + text[i+n:]
	if countNegations(out) != countNegations(text) || digits(out) != digits(text) {
		return text, false
	}
	return out, true
}

// findWords finds phrase in text as whole words, ignoring case, nukta and
// punctuation around words. It returns the byte offset and length of the
// span in text, or -1.
func findWords(text, phrase string) (int, int) {
	want := script.Words(phrase)
	if len(want) == 0 {
		return -1, 0
	}
	type span struct{ start, end int }
	var spans []span
	var folded []string
	for i := 0; i < len(text); {
		for i < len(text) && text[i] == ' ' {
			i++
		}
		j := i
		for j < len(text) && text[j] != ' ' {
			j++
		}
		if j > i {
			w := text[i:j]
			lead := len(w) - len(strings.TrimLeftFunc(w, func(r rune) bool { return unicode.IsPunct(r) }))
			core := trimWord(w)
			if core != "" {
				s := i + lead
				spans = append(spans, span{s, s + len(core)})
				folded = append(folded, script.Fold(core))
			}
		}
		i = j
	}
	for k := 0; k+len(want) <= len(folded); k++ {
		if slices.Equal(folded[k:k+len(want)], want) {
			return spans[k].start, spans[k+len(want)-1].end - spans[k].start
		}
	}
	return -1, 0
}

func containsWords(text, phrase string) bool {
	i, _ := findWords(text, phrase)
	return i >= 0
}

// nearSpan returns the span of text (as many words as sp has) closest to
// sp, if it is within one letter in five of it (and sp is at least five
// letters long), and in the same script; else "".
func nearSpan(text, sp string) string {
	want := []rune(script.Fold(sp))
	if len(want) < 5 || hasDigit(sp) {
		return ""
	}
	n := len(words(sp))
	ws := words(text)
	best, bestD := "", len(want)/5+1
	for k := 0; k+n <= len(ws); k++ {
		cand := strings.Join(ws[k:k+n], " ")
		if hasDigit(cand) || negations[strings.ToLower(cand)] || !sameScript(cand, sp) {
			continue
		}
		if d := editDistance([]rune(script.Fold(cand)), want); d > 0 && d < bestD {
			best, bestD = cand, d
		}
	}
	return best
}

func hasDigit(s string) bool { return strings.IndexFunc(s, unicode.IsDigit) >= 0 }

func scriptOf(r rune) string {
	switch {
	case r < 0x0250:
		return "latin"
	case r >= 0x0900 && r < 0x0D80:
		return string(rune(0x0900 + (r-0x0900)/0x80*0x80)) // the Brahmic block
	}
	return "other"
}

func sameScript(a, b string) bool {
	first := func(s string) string {
		for _, r := range s {
			if unicode.IsLetter(r) {
				return scriptOf(r)
			}
		}
		return ""
	}
	return first(a) == first(b)
}

func editDistance(a, b []rune) int {
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

// vocabulary is what a model's edit may write: bank terms and spellings,
// and the words of the script passage in use.
func vocabulary(entries []bank.Entry, passage string) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		for _, s := range e.Spellings() {
			out[script.Fold(s)] = true
			for _, w := range script.Words(s) {
				out[w] = true
			}
		}
	}
	for _, w := range script.Words(passage) {
		out[w] = true
	}
	return out
}

// justified reports whether every word an edit writes, that it did not
// already have, comes from the bank or the script passage.
func justified(e Edit, allowed map[string]bool) bool {
	if allowed[script.Fold(strings.TrimSpace(e.To))] {
		return true
	}
	had := map[string]bool{}
	for _, w := range script.Words(e.From) {
		had[w] = true
	}
	for _, w := range script.Words(e.To) {
		if !had[w] && !allowed[w] {
			return false
		}
	}
	return true
}

// relevant picks the bank entries worth showing the model for a line: the
// ones with a spelling or mis-hearing near some words of it. At most 30.
func relevant(text string, entries []bank.Entry) []bank.Entry {
	var out []bank.Entry
	for _, e := range entries {
		if e.Status != bank.Approved {
			continue
		}
		for _, s := range append(e.Spellings(), e.Misheard...) {
			if containsWords(text, s) || nearSpan(text, s) != "" || sharesWord(text, s) {
				out = append(out, e)
				break
			}
		}
		if len(out) == 30 {
			break
		}
	}
	return out
}

func sharesWord(text, s string) bool {
	ws := map[string]bool{}
	for _, w := range script.Words(text) {
		if len([]rune(w)) >= 3 {
			ws[w] = true
		}
	}
	for _, w := range script.Words(s) {
		if ws[w] {
			return true
		}
	}
	return false
}
