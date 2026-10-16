package translate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chatServer answers chat completions with answer(user message).
func chatServer(t *testing.T, answer func(user string) string) (*httptest.Server, *[]chatRequest) {
	t.Helper()
	var seen []chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "bad request line", http.StatusNotFound)
			return
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		seen = append(seen, req)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": answer(req.Messages[1].Content)}}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestChatTranslate(t *testing.T) {
	srv, seen := chatServer(t, func(string) string { return " Hola, Nerdearla. \n" })
	c := Chat{URL: srv.URL + "/v1/", Key: "k"}
	got, err := c.Translate(context.Background(), "Hello, Nerdearla.", "en", "es", Keep("Nerdearla"))
	if err != nil || got != "Hola, Nerdearla." {
		t.Fatalf("got %q, %v", got, err)
	}
	req := (*seen)[0]
	if req.Model != DefaultChatModel || req.Temperature != 0 || req.Stream ||
		req.Messages[0].Role != "system" || !strings.Contains(req.Messages[0].Content, "from English to Spanish") ||
		!strings.Contains(req.Messages[0].Content, "Nerdearla") || req.Messages[1].Content != "Hello, Nerdearla." {
		t.Fatalf("request = %+v", req)
	}
}

func TestChatRetriesAnEchoedLine(t *testing.T) {
	const line = "Real time captions help everyone."
	srv, seen := chatServer(t, func(user string) string {
		if user == line {
			return line // the model handed it back untranslated
		}
		return "Los subtítulos en vivo ayudan a todos."
	})
	got, err := Chat{URL: srv.URL + "/v1", Key: "k", Model: "gemma3:4b"}.Translate(context.Background(), line, "en", "es", Glossary{})
	if err != nil || got != "Los subtítulos en vivo ayudan a todos." || len(*seen) != 2 ||
		(*seen)[1].Messages[1].Content != "Translate into Spanish: "+line || (*seen)[1].Model != "gemma3:4b" {
		t.Fatalf("got %q, %v after %+v", got, err, *seen)
	}
}

func TestChatReportsServerErrors(t *testing.T) {
	srv, _ := chatServer(t, func(string) string { return "" })
	_, err := Chat{URL: srv.URL + "/v1", Key: "wrong"}.Translate(context.Background(), "Hi.", "en", "es", Glossary{})
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "translate en→es") {
		t.Fatalf("err = %v", err)
	}
	_, err = Chat{URL: srv.URL + "/v1", Key: "k"}.Translate(context.Background(), "Hi.", "en", "es", Glossary{})
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("empty answer: err = %v", err)
	}
}
