// Copyright 2026 the Dubash contributors. Apache-2.0.

package asr

import (
	"strings"
	"testing"
)

func TestPhraseEnd(t *testing.T) {
	cases := []struct {
		text string
		cuts []string // the text up to each allowed cut
	}{
		// Never after "भाई," nor "तुम क्या": the question is one phrase.
		{"भाई, तुम क्या कर रहे हो यार? चलो अब", []string{"भाई, तुम क्या कर रहे हो यार?"}},
		{"राम जी बोले हम नहीं मानेंगे और रावण चुप रहा", []string{"राम जी बोले", "राम जी बोले हम नहीं मानेंगे"}},
		{"हम बाज़ार जा रहे हैं तो तुम भी चलो", []string{"हम बाज़ार जा रहे हैं"}},
		{"We will not raise prices because the costs fell", []string{"We will not raise prices"}},
	}
	for _, c := range cases {
		words := strings.Fields(c.text)
		var got []string
		for i := range words {
			if phraseEnd(words, i) {
				got = append(got, strings.Join(words[:i+1], " "))
			}
		}
		if strings.Join(got, "|") != strings.Join(c.cuts, "|") {
			t.Errorf("%q: cuts %q, want %q", c.text, got, c.cuts)
		}
	}
}

func TestLastPhraseEnd(t *testing.T) {
	w := strings.Fields("सोई ब्रह्मा जी प्रकट हो गए रुकियो रुकियो हनुमान जी बोले एक तरफ हो जाओ")
	// Stable up to "जी" (10 words), at least 4 words: the last phrase
	// end in range is after "गए".
	if n := lastPhraseEnd(w, 4, 10); strings.Join(w[:n], " ") != "सोई ब्रह्मा जी प्रकट हो गए" {
		t.Errorf("cut after %q", strings.Join(w[:n], " "))
	}
	if n := lastPhraseEnd(strings.Fields("भाई तुम क्या कर"), 1, 3); n != 0 {
		t.Errorf("cut a phrase at %d", n)
	}
}

// TestRunOnCutsAtPhraseEnds: a long unpunctuated Hindi run is cut after a
// clause-final verb, not at the last space.
func TestRunOnCutsAtPhraseEnds(t *testing.T) {
	var a assembler
	text := "आज हम सब यहाँ इकट्ठा हुए हैं क्योंकि हमारे गुरुदेव ने बहुत सुंदर कथा सुनाई थी और उस कथा में बहुत सारी बातें थी जो हमें जीवन में याद रखनी चाहिए हम उन बातों को"
	f := texts(a.interim(text), Final)
	if len(f) == 0 {
		t.Fatal("no cut")
	}
	for _, line := range f {
		words := strings.Fields(line)
		if last := words[len(words)-1]; !clauseFinal[bare(last)] {
			t.Errorf("line ends mid-phrase, on %q: %q", last, line)
		}
	}
}
