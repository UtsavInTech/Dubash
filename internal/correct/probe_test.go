// Copyright 2026 the Dubash contributors. Apache-2.0.

package correct

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/bank"
	"github.com/UtsavInTech/dubash/internal/translate"
)

// TestProbeModelAdversarial runs the brief's adversarial cases through
// the real correction model. Costs paise; by hand:
//
//	SARVAM_PROBE=1 go test -run ProbeModel -v ./internal/correct
func TestProbeModelAdversarial(t *testing.T) {
	key := os.Getenv("SARVAM_API_KEY")
	if key == "" || os.Getenv("SARVAM_PROBE") == "" {
		t.Skip("set SARVAM_API_KEY and SARVAM_PROBE=1")
	}
	script := "नमस्ते दोस्तों। आज बागेश्वर धाम में हनुमंत कथा का पहला दिन है। कथा सुबह 10 बजे शुरू होगी। गुरुदेव धीरेंद्र कृष्ण शास्त्री जी प्रवचन देंगे। प्रवेश निःशुल्क है। हम सब यहाँ इकट्ठा हुए हैं।"
	entries := []bank.Entry{
		entry("बागेश्वर धाम", func(e *bank.Entry) { e.Lang = "hi" }),
		entry("हनुमंत कथा", func(e *bank.Entry) { e.Lang = "hi"; e.Misheard = []string{"हनुमत कथा"} }),
		entry("Apple", func(e *bank.Entry) { e.Ambiguous = true; e.Meaning = "the company" }),
	}
	c := &Corrector{Model: LLM{Chat: translate.SarvamChat{Key: key}}, Deadline: 3 * time.Second}
	for _, line := range []string{
		"आज बागेश्वर धान में हनुमत कथा का पहला दिन है।",        // bank fixes
		"कथा सुबह 11 बजे शुरू होगी।",                           // a number differs from the script: keep 11
		"प्रवेश निःशुल्क नहीं है।",                              // a negation the script lacks: keep it
		"गुरुदेव धीरेंदर किशन शास्त्री जी प्रवचन देंगे।",         // a name only in the script
		"हम लोग रोज़ एक apple खाते हैं।",                         // an ordinary word that is a bank term
		"हम सब यहाँ हुए हैं।",                                   // a word of the script not said
		"हमारी टीम ने पिछले साल 40% कॉस्ट बचाई।",                // Hinglish, off script
		"सोई ब्रह्मा जी प्रकट हो गए रुकियो रुकियो रुकियो।",       // dialect, off script
	} {
		r := c.Correct(context.Background(), Input{Lang: "hi", Text: line, Entries: entries, Script: script, ScriptConf: 0.9})
		t.Logf("\nLINE  %s\nOUT   %s\nEDITS %+v\nREFUSED %+v  (%v, timeout %v)", line, r.Text, r.Edits, r.Skipped, r.Took.Round(time.Millisecond), r.TimedOut)
	}
}
