// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestProbeSarvamLineBreaks checks whether Sarvam keeps one output line
// per input line, which batching several captions in one request needs.
// It costs a few paise; run it by hand:
//
//	SARVAM_PROBE=1 go test -run ProbeSarvamLineBreaks -v ./internal/translate
func TestProbeSarvamLineBreaks(t *testing.T) {
	key := os.Getenv("SARVAM_API_KEY")
	if key == "" || os.Getenv("SARVAM_PROBE") == "" {
		t.Skip("set SARVAM_API_KEY and SARVAM_PROBE=1")
	}
	lines := []string{"हम लोगों की शर्त चल रही है।", "क्या बात है?", "तुम क्यों बीच में आ गए?", "अगर ये छोड़ दोगे।", "तो राम कथा यहीं पूरी हो जाएगी।"}
	s := Sarvam{Key: key}
	for _, dst := range []string{"en", "te", "kn"} {
		for _, sep := range []string{"\n", "\n\n"} {
			out, err := s.call(context.Background(), strings.Join(lines, sep), "hi-IN", map[string]string{"en": "en-IN", "te": "te-IN", "kn": "kn-IN"}[dst])
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Split(strings.TrimSpace(out), sep)
			t.Logf("%s sep=%q: %d lines for %d: %q", dst, sep, len(got), len(lines), got)
		}
	}
}
