package translate

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/genai"
)

// DefaultModel is a fast, cheap text model: captions are short and latency
// matters more than depth.
const DefaultModel = "gemini-3.5-flash-lite"

// Gemini translates with a Gemini text model.
type Gemini struct {
	Client *genai.Client
	Model  string // DefaultModel if empty
}

// Translate implements Translator.
func (g Gemini) Translate(ctx context.Context, text, src, dst string, gl Glossary) (string, error) {
	return translateWith(ctx, g.generate, text, src, dst, gl)
}

func (g Gemini) generate(ctx context.Context, system, text string) (string, error) {
	model := g.Model
	if model == "" {
		model = DefaultModel
	}
	temp := float32(0)
	resp, err := g.Client.Models.GenerateContent(ctx, model, genai.Text(text), &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(system, genai.RoleUser),
		Temperature:       &temp,
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(resp.Text())
	if out == "" {
		return "", errors.New("empty response")
	}
	return out, nil
}
