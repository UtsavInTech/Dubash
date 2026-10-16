// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// TestProbeSarvamPlaceholders asks Sarvam which placeholder shapes survive
// translation untouched. It costs a few paise; run it by hand:
//
//	SARVAM_PROBE=1 go test -run ProbeSarvam -v ./internal/translate
func TestProbeSarvamPlaceholders(t *testing.T) {
	key := os.Getenv("SARVAM_API_KEY")
	if key == "" || os.Getenv("SARVAM_PROBE") == "" {
		t.Skip("set SARVAM_API_KEY and SARVAM_PROBE=1")
	}
	for _, ph := range []string{"[1]", "<1>", "{1}", "__T1__", "ZXQ1", "⟦1⟧", "#1#"} {
		for _, dst := range []string{"hi-IN", "te-IN", "kn-IN"} {
			in := "Welcome to the " + ph + " pilot in Bengaluru, presented by " + ph + "."
			body, _ := json.Marshal(map[string]string{"input": in, "source_language_code": "en-IN", "target_language_code": dst, "model": "sarvam-translate:v1"})
			req, _ := http.NewRequest("POST", "https://api.sarvam.ai/translate", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("api-subscription-key", key)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var out struct {
				Text string `json:"translated_text"`
			}
			json.NewDecoder(resp.Body).Decode(&out)
			resp.Body.Close()
			t.Logf("%-7s %s  kept=%v  %s", ph, dst, bytes.Count([]byte(out.Text), []byte(ph)) == 2, out.Text)
		}
	}
}
