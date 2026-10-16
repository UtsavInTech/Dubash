// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/UtsavInTech/dubash/internal/lang"
)

// SarvamChatURL is Sarvam's chat completions endpoint.
const SarvamChatURL = "https://api.sarvam.ai/v1/chat/completions"

// DefaultSarvamChatModel translated five Hindi lines into Telugu and
// Kannada in 0.4–0.5 s, with the word senses right and approved terms
// kept (probe, 2026-10-16), for about a fiftieth of the translate API's
// price: tokens instead of characters.
const DefaultSarvamChatModel = "sarvam-105b"

// SarvamChat translates with Sarvam's chat model: a numbered batch of
// caption lines in, the same numbers out. Reasoning is switched off (it
// would add latency and billed tokens). It implements Batcher.
type SarvamChat struct {
	Key     string
	URL     string // SarvamChatURL if empty
	Model   string // DefaultSarvamChatModel if empty
	Client  *http.Client
	Limiter *RateLimiter
	// Meter, if set, counts the tokens used, for the cost per event.
	Meter *Meter
	Log   *slog.Logger
	// Fallback translates the lines the model only transliterates.
	Fallback Translator
}

// Meter counts what a provider was billed for.
type Meter struct {
	PromptTokens, CompletionTokens atomic.Int64
	Requests                       atomic.Int64
}

// Translate implements Translator.
func (s SarvamChat) Translate(ctx context.Context, text, src, dst string, g Glossary) (string, error) {
	out, err := s.TranslateBatch(ctx, []string{text}, src, dst, g)
	if err != nil {
		return "", err
	}
	return out[0], nil
}

// chatSystem is the instruction for one language pair.
func chatSystem(src, dst string, g Glossary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You translate live captions of a talk from %s into %s, for people reading on their phones. ", name(src), name(dst))
	b.WriteString("The lines come from speech recognition: they may be spoken style, dialect, or mix English words into an Indian language. ")
	b.WriteString("Translate each numbered line faithfully and completely, keeping its number: one output line per input line, nothing else. ")
	fmt.Fprintf(&b, "Write clear, natural %s as subtitles would. ", name(dst))
	b.WriteString("Never summarize, never add explanations, never invent names, numbers, dates or words that are not in the line; keep every negation and every number exactly. ")
	b.WriteString("A line may be an incomplete sentence: translate it as is. If a word is unclear, keep it as heard rather than guessing. ")
	fmt.Fprintf(&b, "Translate the meaning into %s words: never just write the %s words in %s script. ", name(dst), name(src), name(dst))
	b.WriteString("Never merge or split lines, even when one line continues the previous one.")
	b.WriteString(g.promptRules(dst))
	return b.String()
}

var numberedLine = regexp.MustCompile(`^\s*(\d+)[.)]\s*(.*)$`)

// TranslateBatch implements Batcher.
func (s SarvamChat) TranslateBatch(ctx context.Context, lines []string, src, dst string, g Glossary) ([]string, error) {
	if !lang.Valid(src) || !lang.Valid(dst) {
		return nil, fmt.Errorf("sarvam chat: %s→%s is not supported", src, dst)
	}
	if src == dst {
		return lines, nil
	}
	var in strings.Builder
	if len(lines) > 1 {
		fmt.Fprintf(&in, "%d lines; answer with exactly %d numbered lines.\n", len(lines), len(lines))
	}
	for i, l := range lines {
		fmt.Fprintf(&in, "%d. %s\n", i+1, strings.Join(strings.Fields(l), " "))
	}
	out, err := s.complete(ctx, chatSystem(src, dst, g), in.String(), 64+len(in.String()))
	if err != nil {
		return nil, err
	}
	// No echo check: a code-mixed line said in English is rightly the same
	// in English.
	got, ok := parseNumbered(out, len(lines))
	if !ok {
		s.logf("sarvam chat: answer did not match %d lines: %q", len(lines), out)
		return nil, ErrBatchSplit // the caller retries line by line
	}
	for i := range got {
		if Garbled(lines[i], got[i], dst) {
			got[i] = s.retranslate(ctx, lines, i, src, dst, g, got[i])
		}
	}
	return got, nil
}

// retranslate handles a line the model only wrote out in the target
// script (it happens with dialect it does not understand): once more with
// the neighbouring lines as context, then with the Fallback translator.
// It returns the best it has.
func (s SarvamChat) retranslate(ctx context.Context, lines []string, i int, src, dst string, g Glossary, bad string) string {
	var u strings.Builder
	if i > 0 {
		fmt.Fprintf(&u, "Previous line, for context only: %s\n", lines[i-1])
	}
	if i+1 < len(lines) {
		fmt.Fprintf(&u, "Next line, for context only: %s\n", lines[i+1])
	}
	fmt.Fprintf(&u, "Translate the meaning of this line into %s, in %s words:\n1. %s\n", name(dst), name(dst), lines[i])
	if out, err := s.complete(ctx, chatSystem(src, dst, g), u.String(), 64+2*len(lines[i])); err == nil {
		if t, ok := parseNumbered(out, 1); ok && !Garbled(lines[i], t[0], dst) {
			return t[0]
		}
	}
	if s.Fallback != nil {
		if t, err := s.Fallback.Translate(ctx, lines[i], src, dst, g); err == nil && !Garbled(lines[i], t, dst) {
			s.logf("sarvam chat: line translated by the fallback after transliteration: %q", lines[i])
			return t
		}
	}
	s.logf("sarvam chat: could only transliterate %q into %s", lines[i], dst)
	return bad
}

// parseNumbered reads one translation per line from a model's answer:
// "n. text" lines by number; failing that, as many plain lines as were
// asked for, in order (models sometimes drop the numbers, especially for
// a single line).
func parseNumbered(out string, n int) ([]string, bool) {
	var plain []string
	got := make([]string, n)
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if m := numberedLine.FindStringSubmatch(l); m != nil {
			if k, _ := strconv.Atoi(m[1]); k >= 1 && k <= n && got[k-1] == "" {
				got[k-1] = strings.TrimSpace(m[2])
				plain = append(plain, got[k-1])
				continue
			}
		}
		plain = append(plain, l)
	}
	complete := true
	for _, t := range got {
		if t == "" {
			complete = false
		}
	}
	if complete {
		return got, true
	}
	if len(plain) == n {
		return plain, true
	}
	if n == 1 && len(plain) > 0 { // one line asked, the answer wrapped
		return []string{strings.Join(plain, " ")}, true
	}
	return nil, false
}

func (s SarvamChat) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log.Warn(fmt.Sprintf(format, args...))
	}
}

// Complete asks the chat model for one answer, under the same limiter
// and meter as translations (the correction stage uses it).
func (s SarvamChat) Complete(ctx context.Context, system, user string, maxTokens int) (string, error) {
	return s.complete(ctx, system, user, maxTokens)
}

func (s SarvamChat) complete(ctx context.Context, system, user string, maxTokens int) (string, error) {
	if s.Limiter != nil {
		if err := s.Limiter.Wait(ctx); err != nil {
			return "", err
		}
	}
	model := s.Model
	if model == "" {
		model = DefaultSarvamChatModel
	}
	url := s.URL
	if url == "" {
		url = SarvamChatURL
	}
	body, _ := json.Marshal(map[string]any{
		"model": model, "temperature": 0, "max_tokens": maxTokens,
		"reasoning_effort": nil, // no reasoning: faster, and not billed
		"messages":         []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-subscription-key", s.Key)
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sarvam chat: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("sarvam chat: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Prompt     int64 `json:"prompt_tokens"`
			Completion int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("sarvam chat: decode: %w", err)
	}
	if s.Meter != nil {
		s.Meter.Requests.Add(1)
		s.Meter.PromptTokens.Add(out.Usage.Prompt)
		s.Meter.CompletionTokens.Add(out.Usage.Completion)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("sarvam chat: empty answer")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
