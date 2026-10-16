// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/UtsavInTech/dubash/internal/lang"
)

// SarvamURL is Sarvam's API host.
const SarvamURL = "https://api.sarvam.ai"

// DefaultSarvamModel covers all 22 scheduled languages of India.
const DefaultSarvamModel = "sarvam-translate:v1"

// Sarvam translates with Sarvam's POST /translate.
//
// The API has no glossary parameter (checked 2026-10-16), so terms that
// must not be translated are replaced by numbered placeholders ("[1]")
// before the call and put back after it: Sarvam keeps such placeholders
// in Hindi, Telugu and Kannada (probe in sarvam_probe_test.go). If the
// answer loses a placeholder, the line is translated again without them,
// so no word is dropped from the caption.
type Sarvam struct {
	Key     string
	URL     string // SarvamURL if empty
	Model   string // DefaultSarvamModel if empty
	Client  *http.Client
	Limiter *RateLimiter // shared by every room; nil means no limit
}

// ErrRateLimited is returned when Sarvam answers 429.
var ErrRateLimited = errors.New("sarvam translate: rate limited")

// Translate implements Translator.
func (s Sarvam) Translate(ctx context.Context, text, src, dst string, glossary []string) (string, error) {
	from, to := lang.Sarvam(src), lang.Sarvam(dst)
	if from == "" || to == "" {
		return "", fmt.Errorf("sarvam translate: %s→%s is not supported", src, dst)
	}
	if src == dst {
		return text, nil
	}
	masked, slots := Mask(text, glossary)
	out, err := s.call(ctx, masked, from, to)
	if err != nil {
		return "", err
	}
	restored, ok := Unmask(out, slots)
	if !ok {
		// A placeholder went missing: translate the plain line instead.
		out, err = s.call(ctx, text, from, to)
		if err != nil {
			return "", err
		}
		return out, nil
	}
	return restored, nil
}

// MaxBatchChars bounds one batched request (sarvam-translate:v1 takes
// 2000 characters).
const MaxBatchChars = 1800

// TranslateBatch implements Batcher: the lines go in one request, one per
// line; Sarvam keeps single line breaks (probe in sarvam_probe2_test.go,
// 2026-10-16: 5 of 5 lines in en, te and kn).
func (s Sarvam) TranslateBatch(ctx context.Context, lines []string, src, dst string, glossary []string) ([]string, error) {
	if len(lines) == 1 {
		out, err := s.Translate(ctx, lines[0], src, dst, glossary)
		return []string{out}, err
	}
	from, to := lang.Sarvam(src), lang.Sarvam(dst)
	if from == "" || to == "" {
		return nil, fmt.Errorf("sarvam translate: %s→%s is not supported", src, dst)
	}
	clean := make([]string, len(lines))
	for i, l := range lines {
		clean[i] = strings.Join(strings.Fields(l), " ") // no line breaks inside a line
	}
	masked, slots := Mask(strings.Join(clean, "\n"), glossary)
	out, err := s.call(ctx, masked, from, to)
	if err != nil {
		return nil, err
	}
	restored, ok := Unmask(out, slots)
	parts := strings.Split(strings.TrimSpace(restored), "\n")
	if !ok || len(parts) != len(lines) {
		return nil, ErrBatchSplit
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts, nil
}

func (s Sarvam) call(ctx context.Context, text, from, to string) (string, error) {
	if s.Limiter != nil {
		if err := s.Limiter.Wait(ctx); err != nil {
			return "", err
		}
	}
	model := s.Model
	if model == "" {
		model = DefaultSarvamModel
	}
	base := s.URL
	if base == "" {
		base = SarvamURL
	}
	body, _ := json.Marshal(map[string]string{
		"input": text, "source_language_code": from, "target_language_code": to, "model": model,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/translate", bytes.NewReader(body))
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
		return "", fmt.Errorf("sarvam translate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("sarvam translate: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	var out struct {
		Text string `json:"translated_text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("sarvam translate: decode: %w", err)
	}
	if t := strings.TrimSpace(out.Text); t != "" {
		return t, nil
	}
	return "", errors.New("sarvam translate: empty response")
}

// Mask replaces each occurrence of a term (whole words only, longest term
// first, Latin terms ignoring case) with a numbered placeholder "[n]".
// slots[n-1] is the text to put back: the term as written in the
// glossary.
func Mask(text string, terms []string) (string, []string) {
	ordered := make([]string, 0, len(terms))
	for _, t := range terms {
		if t = strings.TrimSpace(t); t != "" {
			ordered = append(ordered, t)
		}
	}
	// Longest first, so "Sarvam AI" wins over "Sarvam".
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && utf8.RuneCountInString(ordered[j]) > utf8.RuneCountInString(ordered[j-1]); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	var slots []string
	for _, term := range ordered {
		var b strings.Builder
		rest := text
		for {
			i := indexWord(rest, term)
			if i < 0 {
				b.WriteString(rest)
				break
			}
			b.WriteString(rest[:i])
			n := slotOf(slots, term)
			if n == 0 {
				slots = append(slots, term)
				n = len(slots)
			}
			b.WriteString("[" + strconv.Itoa(n) + "]")
			rest = rest[i+len(term):]
		}
		text = b.String()
	}
	return text, slots
}

func slotOf(slots []string, term string) int {
	for i, s := range slots {
		if s == term {
			return i + 1
		}
	}
	return 0
}

// indexWord finds term in s as a whole word: not inside a longer word,
// ignoring case. It returns a byte offset in s, or -1.
func indexWord(s, term string) int {
	ls, lt := strings.ToLower(s), strings.ToLower(term)
	// Lowering can change byte lengths only for rare letters; fall back to
	// an exact search then.
	if len(ls) != len(s) || len(lt) != len(term) {
		ls, lt = s, term
	}
	for from := 0; from <= len(ls); {
		i := strings.Index(ls[from:], lt)
		if i < 0 {
			return -1
		}
		i += from
		before, _ := utf8.DecodeLastRuneInString(s[:i])
		after, _ := utf8.DecodeRuneInString(s[i+len(term):])
		if (i == 0 || !wordRune(before)) && (i+len(term) == len(s) || !wordRune(after)) {
			return i
		}
		from = i + 1
	}
	return -1
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) }

// Unmask puts slots back in place of their placeholders. ok is false
// when a placeholder of slots is missing from s.
func Unmask(s string, slots []string) (string, bool) {
	ok := true
	for i := len(slots); i >= 1; i-- { // [12] before [1]
		ph := "[" + strconv.Itoa(i) + "]"
		if !strings.Contains(s, ph) {
			ok = false
			continue
		}
		s = strings.ReplaceAll(s, ph, slots[i-1])
	}
	return s, ok
}

// RateLimiter keeps requests within perMinute, shared by everything that
// calls one provider account (Sarvam's limits are per account). It lets a
// burst of up to a tenth of the minute's budget go at once (the lines of
// one utterance, in several languages), then spaces requests evenly: the
// generic cell rate algorithm. Spacing every request evenly made a
// five-sentence utterance in three languages wait up to 14 s.
type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration // between requests at the sustained rate
	slack    time.Duration // how far ahead of now the schedule may run
	tat      time.Time     // theoretical arrival time of the next request
}

// NewRateLimiter allows perMinute requests a minute.
func NewRateLimiter(perMinute int) *RateLimiter {
	if perMinute <= 0 {
		return nil
	}
	interval := time.Minute / time.Duration(perMinute)
	burst := max(1, perMinute/10)
	return &RateLimiter{interval: interval, slack: interval * time.Duration(burst-1)}
}

// Wait blocks until the next request may go, or ctx ends.
func (l *RateLimiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	tat := l.tat
	if tat.Before(now) {
		tat = now
	}
	wait := tat.Sub(now) - l.slack
	l.tat = tat.Add(l.interval)
	l.mu.Unlock()
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
