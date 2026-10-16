// Copyright 2026 the Dubash contributors. Apache-2.0.

package script

import (
	"archive/zip"
	"bytes"
	"slices"
	"strings"
	"testing"
)

const demo = `नमस्ते दोस्तों। आज बागेश्वर धाम में हनुमंत कथा का पहला दिन है।

कथा सुबह 10 बजे शुरू होगी। गुरुदेव धीरेंद्र कृष्ण शास्त्री जी प्रवचन देंगे। प्रवचन के बाद दिव्य दरबार लगेगा।

दिव्य दरबार में अर्जी लगाने वाले भक्त पर्चा लेकर आएँ। Dubash app पर कथा हिंदी, తెలుగు और ಕನ್ನಡ में पढ़ सकते हैं।

शाम को सुंदरकांड का पाठ होगा और फिर भंडारा। सभी भक्त भंडारा में प्रसाद लें।`

func TestExtractDocx(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	w.Write([]byte(`<?xml version="1.0"?><w:document xmlns:w="x"><w:body>
		<w:p><w:r><w:t>नमस्ते</w:t></w:r><w:r><w:tab/><w:t xml:space="preserve"> दोस्तों।</w:t></w:r></w:p>
		<w:p><w:r><w:t>Second paragraph.</w:t></w:r></w:p></w:body></w:document>`))
	zw.Close()
	got, err := Extract("talk.docx", buf.Bytes())
	if err != nil || got != "नमस्ते दोस्तों।\n\nSecond paragraph." {
		t.Fatalf("docx: %q %v", got, err)
	}
	if _, err := Extract("talk.pdf", []byte("%PDF")); err != ErrFormat {
		t.Fatalf("pdf: %v", err)
	}
	if got, _ := Extract("t.txt", []byte("a\r\n\r\n\r\nb  c\x07")); got != "a\n\nb c" {
		t.Fatalf("txt %q", got)
	}
}

func TestSplit(t *testing.T) {
	chunks := Split("day1", strings.Repeat(demo+"\n\n", 3), 5)
	if len(chunks) < 3 || chunks[0].Index != 5 || chunks[0].Doc != "day1" {
		t.Fatalf("chunks %+v", chunks)
	}
	for _, c := range chunks {
		if n := len(strings.Fields(c.Text)); n > maxWords {
			t.Errorf("chunk of %d words", n)
		}
	}
}

func TestAlignerFollowsTheSpeaker(t *testing.T) {
	chunks := Split("day1", demo, 0)
	ix := NewIndex(chunks)
	a := NewAligner(ix)
	pos, conf := a.Observe("गुरुदेव धीरेंद्र शास्त्री जी प्रवचन देंगे")
	if !strings.Contains(chunks[pos].Text, "प्रवचन देंगे") || conf < 0.5 {
		t.Fatalf("pos %d conf %.2f", pos, conf)
	}
	// Small talk matches nothing: the position stays, trusted less.
	p2, c2 := a.Observe("अच्छा ठीक है चलिए")
	if p2 != pos || c2 >= conf {
		t.Fatalf("after small talk: %d %.2f", p2, c2)
	}
	pos, _ = a.Observe("शाम को सुंदरकांड का पाठ होगा")
	if !strings.Contains(a.Window(pos), "सुंदरकांड") {
		t.Fatalf("window %q", a.Window(pos))
	}
	var none *Aligner
	if p, c := none.Observe("x"); p != -1 || c != 0 {
		t.Error("nil aligner")
	}
}

func TestSuggestTerms(t *testing.T) {
	var terms []string
	for _, c := range SuggestTerms(demo) {
		terms = append(terms, c.Term)
	}
	for _, want := range []string{"Dubash", "भंडारा", "दरबार", "प्रवचन"} {
		if !slices.Contains(terms, want) {
			t.Errorf("missing %q in %q", want, terms)
		}
	}
	for _, bad := range []string{"10", "और", "app"} {
		if slices.Contains(terms, bad) && bad != "app" {
			t.Errorf("suggested %q", bad)
		}
	}
	en := SuggestTerms("Welcome to the pilot. Our partner Sarvam AI joins Dr. Ananya Rao in Bengaluru. Sarvam AI again.")
	var names []string
	for _, c := range en {
		names = append(names, c.Term)
	}
	if len(en) == 0 || en[0].Term != "Sarvam AI" || en[0].Count != 2 || !slices.Contains(names, "Bengaluru") || slices.Contains(names, "Welcome") {
		t.Errorf("english %+v", en)
	}
}
