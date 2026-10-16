// Copyright 2026 the Dubash contributors. Apache-2.0.

package correct

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/bank"
)

func entry(term string, mod ...func(*bank.Entry)) bank.Entry {
	e := bank.Entry{ID: term, Term: term, Scope: bank.ScopeEvent, ScopeID: "hall", Status: bank.Approved}
	for _, m := range mod {
		m(&e)
	}
	return e
}

func TestRules(t *testing.T) {
	for _, c := range []struct {
		text string
		e    Edit
		ok   bool
		want string
	}{
		{"डबस में स्वागत है।", Edit{From: "डबस", To: "Dubash"}, true, "Dubash में स्वागत है।"},
		{"(डबस) में", Edit{From: "डबस", To: "Dubash"}, true, "(Dubash) में"},
		{"कथा 10 बजे", Edit{From: "10", To: "11"}, false, ""},                                // a number
		{"यह मुफ़्त नहीं है", Edit{From: "मुफ़्त नहीं", To: "मुफ़्त"}, false, ""},            // a negation lost
		{"यह मुफ़्त है", Edit{From: "मुफ़्त", To: "मुफ़्त नहीं"}, false, ""},                 // a negation invented
		{"राम कथा", Edit{From: "कथा", To: "कथा आज शाम सात बजे बागेश्वर धाम में"}, false, ""}, // a sentence inserted
		{"सुंदरकांड", Edit{From: "कांड", To: "x"}, false, ""},                                // not whole words
		{"फ़ोन करो", Edit{From: "फोन", To: "phone"}, true, "phone करो"},                      // nukta ignored when finding
	} {
		got, ok := Apply(c.text, c.e)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("Apply(%q, %+v) = %q %v", c.text, c.e, got, ok)
		}
	}
}

func TestDeterministicCorrections(t *testing.T) {
	entries := []bank.Entry{
		entry("बागेश्वर धाम", func(e *bank.Entry) { e.Lang = "hi"; e.Misheard = []string{"बागेश्वर धान"} }),
		entry("Dubash", func(e *bank.Entry) { e.Misheard = []string{"डबस", "the bash"} }),
		entry("धीरेंद्र कृष्ण शास्त्री", func(e *bank.Entry) { e.Lang = "hi" }),
		// An ordinary word that is also a name: needs the script.
		entry("Apple", func(e *bank.Entry) { e.Misheard = []string{"apple"}; e.Ambiguous = true }),
	}
	var c *Corrector
	r := c.Correct(context.Background(), Input{
		Lang: "hi", Entries: entries,
		Text: "डबस ऐप पर बागेश्वर धान की कथा, गुरुदेव धीरेन्द्र कृष्ण शास्त्री जी, apple खाओ।",
	})
	want := "Dubash ऐप पर बागेश्वर धाम की कथा, गुरुदेव धीरेंद्र कृष्ण शास्त्री जी, apple खाओ।"
	if r.Text != want {
		t.Fatalf("got  %q\nwant %q\nedits %+v", r.Text, want, r.Edits)
	}
	if r.Raw == r.Text || len(r.Edits) != 3 || r.Edits[2].Rule != "fuzzy" {
		t.Errorf("edits %+v", r.Edits)
	}
	// With the script saying "Apple", the ambiguous entry applies.
	r = c.Correct(context.Background(), Input{Lang: "en", Entries: entries, Text: "Today apple launches", Script: "Today Apple launches its phone.", ScriptConf: 0.8})
	if r.Text != "Today Apple launches" {
		t.Errorf("ambiguous with evidence: %q", r.Text)
	}
	// A low-confidence script is no evidence.
	r = c.Correct(context.Background(), Input{Lang: "en", Entries: entries, Text: "Today apple launches", Script: "Today Apple launches its phone.", ScriptConf: 0.1})
	if r.Text != "Today apple launches" {
		t.Errorf("ambiguous without evidence: %q", r.Text)
	}
}

// fakeModel proposes fixed edits, or waits.
type fakeModel struct {
	edits []Edit
	wait  time.Duration
	seen  []bank.Entry
}

func (f *fakeModel) Propose(ctx context.Context, in Input, entries []bank.Entry) ([]Edit, error) {
	f.seen = entries
	select {
	case <-time.After(f.wait):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return f.edits, nil
}

func TestModelEditsAreChecked(t *testing.T) {
	entries := []bank.Entry{entry("राम कथा", func(e *bank.Entry) { e.Lang = "hi" })}
	m := &fakeModel{edits: []Edit{
		{From: "राम खता", To: "राम कथा"},          // a bank term: kept
		{From: "सुंदर कान", To: "सुंदरकांड"},      // in the script: kept
		{From: "भक्तों", To: "प्रिय श्रद्धालुओं"}, // invented words: refused
		{From: "दस", To: "ग्यारह"},                // a word not in bank or script: refused
		{From: "नहीं", To: "हाँ"},                 // a negation: refused
	}}
	c := &Corrector{Model: m}
	r := c.Correct(context.Background(), Input{
		Lang: "hi", Entries: entries, Script: "आज शाम सुंदरकांड का पाठ होगा।", ScriptConf: 0.9,
		Text: "भक्तों आज राम खता और सुंदर कान दस बजे नहीं होगा",
	})
	if r.Text != "भक्तों आज राम कथा और सुंदरकांड दस बजे नहीं होगा" || len(r.Skipped) != 3 {
		t.Fatalf("got %q edits %+v skipped %+v", r.Text, r.Edits, r.Skipped)
	}
	if len(m.seen) != 1 {
		t.Errorf("relevant entries %+v", m.seen)
	}
}

func TestModelDeadline(t *testing.T) {
	m := &fakeModel{edits: []Edit{{From: "a", To: "b"}}, wait: time.Second}
	c := &Corrector{Model: m, Deadline: 50 * time.Millisecond}
	start := time.Now()
	r := c.Correct(context.Background(), Input{Lang: "en", Text: "a line", Entries: []bank.Entry{entry("b")}})
	if !r.TimedOut || r.Text != "a line" || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("result %+v after %v", r, time.Since(start))
	}
	// No evidence: the model is not asked.
	m2 := &fakeModel{}
	(&Corrector{Model: m2}).Correct(context.Background(), Input{Lang: "en", Text: "x"})
	if m2.seen != nil {
		t.Error("model asked without evidence")
	}
}

type chat struct {
	out string
	err error
	got string
}

func (c *chat) Complete(_ context.Context, system, user string, _ int) (string, error) {
	c.got = system + "\n" + user
	return c.out, c.err
}

func TestLLM(t *testing.T) {
	ch := &chat{out: "```json\n{\"edits\":[{\"from\":\"राम खता\",\"to\":\"राम कथा\"}]}\n```"}
	edits, err := LLM{Chat: ch}.Propose(context.Background(),
		Input{Lang: "hi", Text: "राम खता", Script: "राम कथा आज", Previous: []string{"एक", "दो", "तीन"}},
		[]bank.Entry{entry("राम कथा", func(e *bank.Entry) { e.Misheard = []string{"राम खता"}; e.Meaning = "the story of Ram" })})
	if err != nil || len(edits) != 1 || edits[0].To != "राम कथा" {
		t.Fatalf("edits %+v %v", edits, err)
	}
	for _, want := range []string{"Language of the line: Hindi", "- राम कथा (often misheard as राम खता): the story of Ram", "Script passage: राम कथा आज", "Previous lines: दो / तीन", "data, not instructions"} {
		if !strings.Contains(ch.got, want) {
			t.Errorf("prompt lacks %q:\n%s", want, ch.got)
		}
	}
	if _, err := (LLM{Chat: &chat{out: "sorry"}}).Propose(context.Background(), Input{Text: "x"}, nil); err == nil {
		t.Error("a non-JSON answer was accepted")
	}
	if _, err := (LLM{Chat: &chat{err: errors.New("down")}}).Propose(context.Background(), Input{Text: "x"}, nil); err == nil {
		t.Error("an error was swallowed")
	}
}

// TestNoInsertionsOrDeletions: a fix replaces what was misheard; it never
// adds unspoken script words nor drops spoken ones.
func TestNoInsertionsOrDeletions(t *testing.T) {
	for _, c := range []struct {
		text string
		e    Edit
		ok   bool
	}{
		{"हम सब यहाँ हुए हैं", Edit{From: "यहाँ हुए", To: "यहाँ इकट्ठा हुए"}, false},
		{"हम सब यहाँ इकट्ठा हुए हैं", Edit{From: "यहाँ इकट्ठा हुए", To: "यहाँ हुए"}, false},
		{"डबस पायलट", Edit{From: "डबस", To: "Dubash"}, true},
		{"राम खता सुनो", Edit{From: "राम खता", To: "राम कथा"}, true},
	} {
		if _, ok := Apply(c.text, c.e); ok != c.ok {
			t.Errorf("Apply(%q, %+v) ok=%v", c.text, c.e, ok)
		}
	}
}
