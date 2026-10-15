// Copyright 2026 the lenguaraz India fork contributors. Apache-2.0.

package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RchrdHndrcks/lenguaraz/internal/asr"
)

// SarvamURL is Sarvam's API host.
const SarvamURL = "https://api.sarvam.ai"

// Sarvam keyterm limits (docs.sarvam.ai, keyterm prompting guide, read
// 2026-10-16): at most 50 terms of at most 64 characters, saaras:v4 only.
const (
	sarvamMaxKeyterms   = 50
	sarvamMaxKeytermLen = 64
)

// sarvamMaxClip keeps each REST request under the 30 s the REST API is
// meant for; longer recordings are cut at the quietest moment between
// sarvamMinClip and sarvamMaxClip.
const (
	sarvamMinClip = 15 * time.Second
	sarvamMaxClip = 28 * time.Second
)

// SarvamREST transcribes with Sarvam's file API (POST /speech-to-text),
// one request per clip of at most 28 s. It is the benchmark's baseline
// for Sarvam's recognition accuracy and keyterm effect; it is not the
// streaming path. Streaming latency is measured through EngineRecognizer
// once the Sarvam streaming engine exists (phase 1).
type SarvamREST struct {
	Key   string
	URL   string // SarvamURL if empty
	Model string // saaras:v4 if empty: the only model that takes keyterms
	// Mode is transcribe, codemix, verbatim, translit or translate; empty
	// leaves the API default. The REST docs (2026-10-16) list mode for
	// saaras:v3 only.
	Mode   string
	Client *http.Client
}

// Name implements Recognizer.
func (s SarvamREST) Name() string {
	if s.Mode != "" {
		return "sarvam-rest:" + s.model() + ":" + s.Mode
	}
	return "sarvam-rest:" + s.model()
}

func (s SarvamREST) model() string {
	if s.Model != "" {
		return s.Model
	}
	return "saaras:v4"
}

func (s SarvamREST) url() string {
	if s.URL != "" {
		return strings.TrimRight(s.URL, "/")
	}
	return SarvamURL
}

func (s SarvamREST) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Keyterms trims vocab to what Sarvam accepts and reports what it left
// out, so the report says when a vocabulary was cut.
func Keyterms(vocab []string) (kept, dropped []string) {
	for _, t := range vocab {
		switch {
		case utf8.RuneCountInString(t) > sarvamMaxKeytermLen, len(kept) == sarvamMaxKeyterms:
			dropped = append(dropped, t)
		default:
			kept = append(kept, t)
		}
	}
	return kept, dropped
}

// Recognize implements Recognizer.
func (s SarvamREST) Recognize(ctx context.Context, pcm []byte, lang string, vocab []string) (Recognition, error) {
	var rec Recognition
	keyterms, dropped := Keyterms(vocab)
	if len(dropped) > 0 {
		rec.Errors = append(rec.Errors, fmt.Sprintf("%d vocabulary terms not sent (Sarvam takes %d terms of up to %d characters)", len(dropped), sarvamMaxKeyterms, sarvamMaxKeytermLen))
	}
	var texts []string
	start := time.Now()
	for _, clip := range Clips(pcm, sarvamMinClip, sarvamMaxClip) {
		t0 := time.Now()
		text, err := s.transcribe(ctx, clip, lang, keyterms)
		if err != nil {
			return rec, err
		}
		now := time.Now()
		rec.Lags = append(rec.Lags, now.Sub(t0))
		if rec.FirstFinal == 0 {
			rec.FirstFinal = now.Sub(start)
		}
		// Billed per second, rounded up, per request.
		rec.BilledSeconds += math.Ceil(Seconds(clip))
		texts = append(texts, text)
	}
	rec.Text = strings.Join(texts, " ")
	if n := len(rec.Lags); n > 0 {
		rec.Tail = rec.Lags[n-1]
	}
	return rec, nil
}

func (s SarvamREST) transcribe(ctx context.Context, pcm []byte, lang string, keyterms []string) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "clip.wav")
	if err != nil {
		return "", err
	}
	fw.Write(EncodeWAV(pcm))
	mw.WriteField("model", s.model())
	mw.WriteField("language_code", BCP47(lang))
	if s.Mode != "" {
		mw.WriteField("mode", s.Mode)
	}
	if len(keyterms) > 0 {
		kt, _ := json.Marshal(keyterms)
		mw.WriteField("keyterms", string(kt))
	}
	mw.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url()+"/speech-to-text", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("api-subscription-key", s.Key)
	var out struct {
		Transcript string `json:"transcript"`
	}
	if err := doJSON(s.client(), req, &out); err != nil {
		return "", fmt.Errorf("sarvam speech-to-text: %w", err)
	}
	return strings.TrimSpace(out.Transcript), nil
}

// Clips cuts pcm into pieces no longer than maxLen, each cut at the
// quietest 100 ms window after minLen, so words are rarely split.
func Clips(pcm []byte, minLen, maxLen time.Duration) [][]byte {
	bytesOf := func(d time.Duration) int { return int(d.Seconds()*asr.BytesPerSecond) &^ 1 }
	lo, hi := bytesOf(minLen), bytesOf(maxLen)
	var out [][]byte
	for len(pcm) > hi {
		cut, quietest := hi, math.Inf(1)
		for off := lo; off+chunkBytes <= hi; off += chunkBytes / 2 {
			if rms := asr.RMS(pcm[off : off+chunkBytes]); rms < quietest {
				cut, quietest = off+chunkBytes/2, rms
			}
		}
		cut &^= 1
		out = append(out, pcm[:cut])
		pcm = pcm[cut:]
	}
	if len(pcm) > 0 {
		out = append(out, pcm)
	}
	return out
}

// SarvamTranslator translates with Sarvam's POST /translate. The API has
// no glossary parameter (checked 2026-10-16), so the glossary argument is
// ignored here: the benchmark shows how often such terms survive anyway,
// and phase 2 protects them in our own code before calling the API.
type SarvamTranslator struct {
	Key    string
	URL    string // SarvamURL if empty
	Model  string // sarvam-translate:v1 if empty (22 languages, 2000 characters)
	Client *http.Client
}

// Translate implements translate.Translator.
func (s SarvamTranslator) Translate(ctx context.Context, text, src, dst string, _ []string) (string, error) {
	model := s.Model
	if model == "" {
		model = "sarvam-translate:v1"
	}
	base := s.URL
	if base == "" {
		base = SarvamURL
	}
	body, _ := json.Marshal(map[string]string{
		"input": text, "source_language_code": BCP47(src), "target_language_code": BCP47(dst), "model": model,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/translate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-subscription-key", s.Key)
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	var out struct {
		TranslatedText string `json:"translated_text"`
	}
	if err := doJSON(client, req, &out); err != nil {
		return "", fmt.Errorf("sarvam translate %s→%s: %w", src, dst, err)
	}
	if strings.TrimSpace(out.TranslatedText) == "" {
		return "", errors.New("sarvam translate: empty response")
	}
	return strings.TrimSpace(out.TranslatedText), nil
}

// doJSON sends req and decodes a 200 response into out; other statuses
// become errors carrying the start of the body (never the request, which
// holds the key in a header).
func doJSON(c *http.Client, req *http.Request, out any) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
