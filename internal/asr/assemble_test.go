package asr

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// texts returns the text of every event of kind k.
func texts(evs []Event, k Kind) []string {
	var out []string
	for _, ev := range evs {
		if ev.Kind == k {
			out = append(out, ev.Text)
		}
	}
	return out
}

func lastInterim(t *testing.T, evs []Event) string {
	t.Helper()
	in := texts(evs, Interim)
	if len(in) == 0 {
		t.Fatalf("no interim in %+v", evs)
	}
	return in[len(in)-1]
}

func TestSentenceEnds(t *testing.T) {
	cases := []struct {
		text string
		want []string // complete sentences, in order
	}{
		{"Hello world", nil},
		{"Hello world.", []string{"Hello world."}},
		{"One. Two? Three! Four", []string{"One.", " Two?", " Three!"}},
		{"Gemini 3.5 is here. Yes", []string{"Gemini 3.5 is here."}},
		{"Tools, e.g. Go, etc. are fine. Next", []string{"Tools, e.g. Go, etc. are fine."}},
		{"Ask Dr. Smith. Now", []string{"Ask Dr. Smith."}},
		{"Wait... what?! Ok", []string{"Wait...", " what?!"}},
		{`He said "stop." Then`, []string{`He said "stop."`}},
		{"¿Hola? Sí… claro", []string{"¿Hola?", " Sí…"}},
	}
	for _, c := range cases {
		var got []string
		prev := 0
		for _, end := range sentenceEnds(c.text) {
			got = append(got, c.text[prev:end])
			prev = end
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("sentenceEnds(%q) split %q, want %q", c.text, got, c.want)
		}
	}
}

func TestAssemblerCommitsStableSentencesFromInterims(t *testing.T) {
	var a assembler
	steps := []struct {
		in      string
		finals  []string
		interim string
	}{
		{"Welcome.", nil, "Welcome."},
		{"Welcome to Nerdearla.", nil, "Welcome to Nerdearla."},
		{"Welcome to Nerdearla. Today", nil, "Welcome to Nerdearla. Today"},
		{"Welcome to Nerdearla. Today, we", []string{"Welcome to Nerdearla."}, "Today, we"},
		{"Welcome to Nerdearla. Today, we are going to talk to", nil, "Today, we are going to talk to"},
		{"Welcome to Nerdearla. Today, we are going to talk about it. Yes. It is", []string{"Today, we are going to talk about it.", "Yes."}, "It is"},
	}
	for i, s := range steps {
		evs := a.interim(s.in)
		if got := texts(evs, Final); strings.Join(got, "|") != strings.Join(s.finals, "|") {
			t.Fatalf("step %d: finals %q, want %q", i, got, s.finals)
		}
		if got := lastInterim(t, evs); got != s.interim {
			t.Fatalf("step %d: interim %q, want %q", i, got, s.interim)
		}
		if n := len(evs); n > 0 && evs[n-1].Kind != Interim {
			t.Fatalf("step %d: the interim must come after the finals: %+v", i, evs)
		}
	}
}

func TestAssemblerUtteranceFinalEmitsOnlyTheUncommittedTail(t *testing.T) {
	var a assembler
	a.interim("First one. Second one is")
	evs := a.final("First one! Second one is done.") // text differs slightly
	if got := texts(evs, Final); strings.Join(got, "|") != "Second one is done." {
		t.Fatalf("finals %q", got)
	}
	// The next utterance starts from scratch.
	if got := texts(a.interim("Third. New one here"), Final); strings.Join(got, "|") != "Third." {
		t.Fatalf("next utterance finals %q", got)
	}
}

func TestAssemblerCarriesAnUnfinishedUtteranceOnce(t *testing.T) {
	var a assembler
	if got := texts(a.final("Done here. And then in less than"), Final); strings.Join(got, "|") != "Done here." {
		t.Fatalf("finals %q", got)
	}
	if got := lastInterim(t, a.interim("a second")); got != "And then in less than a second" {
		t.Fatalf("interim with carry = %q", got)
	}
	if got := texts(a.final("a second. Kubernetes and"), Final); strings.Join(got, "|") != "And then in less than a second." {
		t.Fatalf("carry not joined: %q", got)
	}
	// The new carry is not emitted yet; a carry that already waited one
	// utterance is, even without punctuation.
	if got := texts(a.final("more words"), Final); strings.Join(got, "|") != "Kubernetes and more words" {
		t.Fatalf("stale carry not flushed: %q", got)
	}
	if evs := a.flush(); len(evs) != 0 {
		t.Fatalf("nothing should be left, got %+v", evs)
	}
}

func TestAssemblerCarryIsPrependedToTheFirstEarlyCommit(t *testing.T) {
	var a assembler
	a.final("We talk about")
	evs := a.interim("open source. And more text")
	if got := texts(evs, Final); strings.Join(got, "|") != "We talk about open source." {
		t.Fatalf("finals %q", got)
	}
	if got := lastInterim(t, evs); got != "And more text" {
		t.Fatalf("interim %q", got)
	}
}

func TestAssemblerFlushEmitsCarryAndUncommittedInterim(t *testing.T) {
	var a assembler
	a.final("left over")
	a.interim("words in flight")
	if got := texts(a.flush(), Final); strings.Join(got, "|") != "left over words in flight" {
		t.Fatalf("flush = %q", got)
	}
	if evs := a.flush(); len(evs) != 0 {
		t.Fatalf("second flush = %+v", evs)
	}
}

func TestAssemblerEndSessionCarriesUncommittedInterim(t *testing.T) {
	var a assembler
	a.interim("Done. Half a sentence")
	a.endSession() // no utterance final came before the session closed
	// A new session's interims start from scratch.
	evs := a.interim("that goes on. Next one")
	if got := texts(evs, Final); strings.Join(got, "|") != "Half a sentence that goes on." {
		t.Fatalf("finals %q", got)
	}
}

func TestAssemblerRunOnGuard(t *testing.T) {
	var a assembler
	clause := "and then we kept talking about many things"
	long := strings.Repeat(clause+", ", 5) + "without ever stopping"
	evs := a.interim(long)
	finals := texts(evs, Final)
	if len(finals) != 1 {
		t.Fatalf("finals %q", finals)
	}
	want := strings.Repeat(clause+", ", 4) + clause + ","
	if finals[0] != want {
		t.Fatalf("committed %q\nwant      %q", finals[0], want)
	}
	if got := lastInterim(t, evs); got != "without ever stopping" {
		t.Fatalf("interim %q", got)
	}
	// The utterance final skips the words already committed.
	if got := texts(a.final(long+" at all."), Final); strings.Join(got, "|") != "without ever stopping at all." {
		t.Fatalf("final %q", got)
	}
}

// Modified by the Dubash contributors: without a phrase end the guard
// now waits for twice maxPending before cutting at a space (60 words used
// to be enough), so that lines are not cut mid-phrase.
func TestAssemblerRunOnGuardWithoutCommas(t *testing.T) {
	var a assembler
	long := strings.TrimSpace(strings.Repeat("word ", 100))
	finals := texts(a.interim(long), Final)
	if len(finals) != 1 || len(finals[0]) > maxPending || !strings.HasPrefix(long, finals[0]) {
		t.Fatalf("finals %q", finals)
	}
}

func TestAssemblerJoinsChunks(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"Hello", "world", "Hello world"},
		{"Hello ", "world", "Hello world"},
		{"Hello", " world", "Hello world"},
		{"Hello", ", world", "Hello, world"},
	}
	for _, c := range cases {
		if got := join(c.a, c.b); got != c.want {
			t.Errorf("join(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

type recorded struct {
	at    float64
	final bool
	text  string
}

// loadRecording reads events printed by cmd/asr-smoke against the real
// Live API: "<seconds>s interim|FINAL <cumulative utterance text>".
func loadRecording(t *testing.T, path string) []recorded {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []recorded
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		ts, rest, _ := strings.Cut(line, " ")
		kind, text, _ := strings.Cut(strings.TrimSpace(rest), " ")
		at, err := strconv.ParseFloat(strings.TrimSuffix(ts, "s"), 64)
		if err != nil {
			t.Fatalf("bad line %q", line)
		}
		out = append(out, recorded{at: at, final: kind == "FINAL", text: strings.TrimSpace(text)})
	}
	return out
}

// replay feeds a recording to an assembler, stamping each final with the
// time of the message that produced it (-1 for the closing flush).
func replay(t *testing.T, path string) (finals []Event, utterances []string) {
	t.Helper()
	var a assembler
	for _, r := range loadRecording(t, path) {
		var evs []Event
		if r.final {
			utterances = append(utterances, r.text)
			evs = a.final(r.text)
		} else {
			evs = a.interim(r.text)
		}
		for _, ev := range evs {
			if ev.Kind == Final {
				ev.At = r.at
				finals = append(finals, ev)
			}
		}
	}
	for _, ev := range a.flush() {
		ev.At = -1
		finals = append(finals, ev)
	}
	return finals, utterances
}

func joinedTexts(evs []Event) string {
	var all []string
	for _, ev := range evs {
		all = append(all, ev.Text)
	}
	return strings.Join(all, " ")
}

func TestAssemblerReplaysRecordedEnglishSession(t *testing.T) {
	finals, utterances := replay(t, "testdata/live-en.txt")
	want := []struct {
		at   float64
		text string
	}{
		{2.7, "Welcome to Nerdearla."}, // not at 21.4 s, when the server closed the utterance
		{9.0, "Today, we are going to talk about how open source communities make conferences more accessible."},
		// A long sentence is committed at a clause boundary, not at its end.
		{14.3, "Real-time captions help people who are deaf or hard of hearing, people who are learning English,"},
		{16.7, "and anyone sitting in the back of a noisy room."},
		// The first utterance ended mid-sentence at 21.4 s: carried over.
		{27.6, "With Gemini, we can transcribe a talk as it happens and translate it into Spanish in less than Second, Kubernetes, eBPF, and WebAssembly are the kind of words a good glossary should protect."},
	}
	if len(finals) != len(want) {
		t.Fatalf("got %d finals, want %d: %+v", len(finals), len(want), finals)
	}
	seen := map[string]bool{}
	for i, w := range want {
		if finals[i].Text != w.text || finals[i].At != w.at {
			t.Errorf("final %d = %.1fs %q\nwant       %.1fs %q", i, finals[i].At, finals[i].Text, w.at, w.text)
		}
		if seen[finals[i].Text] {
			t.Errorf("final emitted twice: %q", finals[i].Text)
		}
		seen[finals[i].Text] = true
	}
	// No text lost: the finals add up to the server's utterance finals.
	if got, want := joinedTexts(finals), strings.Join(utterances, " "); got != want {
		t.Errorf("finals lost or changed text:\n got %q\nwant %q", got, want)
	}
}

func TestAssemblerReplaysRecordedSpanishSession(t *testing.T) {
	finals, utterances := replay(t, "testdata/live-es.txt")
	want := []float64{3.1, 8.8, 14.1, 18.4, 21.9} // the long third sentence goes out in two pieces
	if len(finals) != len(want) {
		t.Fatalf("got %d finals, want %d: %+v", len(finals), len(want), finals)
	}
	for i, at := range want {
		if finals[i].At != at {
			t.Errorf("final %d at %.1fs, want %.1fs: %q", i, finals[i].At, at, finals[i].Text)
		}
	}
	if finals[0].Text != "Bienvenidos a Nerdearla." {
		t.Errorf("first final = %q", finals[0].Text)
	}
	if got, want := joinedTexts(finals), strings.Join(utterances, " "); got != want {
		t.Errorf("finals lost or changed text:\n got %q\nwant %q", got, want)
	}
}

func TestAssemblerSettlesAFinishedSentence(t *testing.T) {
	var a assembler
	a.interim("Welcome to Nerdearla. Today we")
	if evs := a.settle(); len(evs) != 0 {
		t.Fatalf("settled an unfinished sentence: %+v", evs)
	}
	a.interim("Welcome to Nerdearla. Today we talk about captions.")
	if got := texts(a.settle(), Final); strings.Join(got, "|") != "Today we talk about captions." {
		t.Fatalf("settled %q", got)
	}
	// The same interim again, and the server's utterance final, add nothing.
	if evs := a.interim("Welcome to Nerdearla. Today we talk about captions."); len(texts(evs, Final)) != 0 {
		t.Fatalf("interim re-emitted %+v", evs)
	}
	if got := texts(a.final("Welcome to Nerdearla. Today we talk about captions."), Final); len(got) != 0 {
		t.Fatalf("final re-emitted %q", got)
	}
	// The speaker goes on: only the new words are pending.
	evs := a.interim("And one more thing")
	if got := lastInterim(t, evs); got != "And one more thing" {
		t.Fatalf("interim %q", got)
	}
}
