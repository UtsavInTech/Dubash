// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestProbeSarvamChat compares Sarvam's chat models as caption
// translators: latency, tokens (cost) and output. Costs paise; by hand:
//
//	SARVAM_PROBE=1 go test -run ProbeSarvamChat -v ./internal/translate
func TestProbeSarvamChat(t *testing.T) {
	key := os.Getenv("SARVAM_API_KEY")
	if key == "" || os.Getenv("SARVAM_PROBE") == "" {
		t.Skip("set SARVAM_API_KEY and SARVAM_PROBE=1")
	}
	lines := []string{"हम लोगों की शर्त चल रही है।", "क्या बात है?", "तुम क्यों बीच में आ गए?", "अगर ये छोड़ दोगे।", "तो राम कथा यहीं पूरी हो जाएगी।"}
	var numbered strings.Builder
	for i, l := range lines {
		numbered.WriteString(strings.Repeat("", 0))
		numbered.WriteString(string(rune('1'+i)) + ". " + l + "\n")
	}
	for _, model := range []string{"sarvam-105b-conversations", "sarvam-105b"} {
		for _, dst := range []string{"Telugu", "Kannada"} {
			body, _ := json.Marshal(map[string]any{
				"model": model, "temperature": 0, "max_tokens": 600, "reasoning_effort": nil,
				"messages": []map[string]string{
					{"role": "system", "content": "You translate live captions of a Hindi religious discourse into " + dst + ". Translate each numbered line faithfully, keeping its number; output only the numbered translations, one per line. Never invent names, numbers or dates. Keep: राम कथा → రామకథ / ರಾಮಕಥೆ."},
					{"role": "user", "content": numbered.String()},
				},
			})
			req, _ := http.NewRequest("POST", "https://api.sarvam.ai/v1/chat/completions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("api-subscription-key", key)
			start := time.Now()
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var out struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					}
				}
				Usage map[string]any
			}
			raw := new(bytes.Buffer)
			raw.ReadFrom(resp.Body)
			resp.Body.Close()
			json.Unmarshal(raw.Bytes(), &out)
			took := time.Since(start)
			text := ""
			if len(out.Choices) > 0 {
				text = out.Choices[0].Message.Content
			} else {
				text = raw.String()
			}
			t.Logf("%s → %s: %v %s usage=%v\n%s", model, dst, took.Round(time.Millisecond), resp.Status, out.Usage, text)
		}
	}
}
