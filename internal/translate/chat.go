package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DefaultChatModel is Gemma 3 as Ollama names it: an open model that runs
// on the venue's own hardware.
const DefaultChatModel = "gemma3"

// Chat translates with any server that implements the OpenAI chat
// completions API (POST {URL}/chat/completions): Ollama, llama.cpp's
// server, vLLM or LocalAI running Gemma or another open model on the
// venue's own machine, or a hosted provider.
type Chat struct {
	URL    string       // base URL, such as http://localhost:11434/v1
	Model  string       // DefaultChatModel if empty
	Key    string       // bearer token, if the server wants one
	Client *http.Client // http.DefaultClient if nil
}

// Translate implements Translator.
func (c Chat) Translate(ctx context.Context, text, src, dst string, g Glossary) (string, error) {
	return translateWith(ctx, c.generate, text, src, dst, g)
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func (c Chat) generate(ctx context.Context, system, text string) (string, error) {
	model := c.Model
	if model == "" {
		model = DefaultChatModel
	}
	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: text},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("no choices in response")
	}
	answer := strings.TrimSpace(out.Choices[0].Message.Content)
	if answer == "" {
		return "", errors.New("empty response")
	}
	return answer, nil
}
