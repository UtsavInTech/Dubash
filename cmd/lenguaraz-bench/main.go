// Copyright 2026 the lenguaraz India fork contributors. Apache-2.0.

// Command lenguaraz-bench measures speech recognition and translation
// providers on recorded audio, with and without a vocabulary list: word
// and character error rate, vocabulary term accuracy, wrongly inserted
// terms, numbers, negations, latency and cost. See docs/TESTING.md.
//
//	lenguaraz-bench -asr scripted -translate fake            # no API calls
//	lenguaraz-bench -asr sarvam -translate sarvam -to hi,te,kn
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/RchrdHndrcks/lenguaraz/internal/asr"
	"github.com/RchrdHndrcks/lenguaraz/internal/bench"
	"github.com/RchrdHndrcks/lenguaraz/internal/translate"
)

func main() {
	dir := flag.String("dir", "bench", "benchmark directory: recordings/, vocab/, pricing.yaml")
	asrList := flag.String("asr", "scripted", "recognizers, comma separated: scripted, sarvam, gemini, whisper")
	mtList := flag.String("translate", "fake", "translators, comma separated: fake, sarvam, gemini, chat (empty for none)")
	to := flag.String("to", "hi,te,kn", "target languages")
	vocab := flag.String("vocab", "both", "both (baseline and with vocabulary), on or off")
	only := flag.String("only", "", "only recordings whose name contains this")
	speed := flag.Float64("speed", 1, "pace for streaming engines: 1 real time, 0 as fast as possible")
	out := flag.String("out", "", "results directory (default DIR/results/TIMESTAMP)")
	budget := flag.Float64("budget", 10, "refuse to run when the estimated cost exceeds this many rupees")
	yes := flag.Bool("yes", false, "run even above -budget")
	flag.Parse()

	if err := run(*dir, *asrList, *mtList, *to, *vocab, *only, *speed, *out, *budget, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "lenguaraz-bench:", err)
		os.Exit(1)
	}
}

func run(dir, asrList, mtList, to, vocab, only string, speed float64, out string, budget float64, yes bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	recs, err := bench.LoadRecordings(filepath.Join(dir, "recordings"))
	if err != nil {
		return err
	}
	if only != "" {
		var kept []bench.Recording
		for _, r := range recs {
			if strings.Contains(r.Name, only) {
				kept = append(kept, r)
			}
		}
		recs = kept
	}
	if len(recs) == 0 {
		return fmt.Errorf("no recordings in %s/recordings: add NAME.wav with NAME.yaml (see bench/README.md)", dir)
	}
	pricing, err := bench.LoadPricing(filepath.Join(dir, "pricing.yaml"))
	if err != nil {
		return err
	}
	recognizers, err := recognizers(ctx, split(asrList), speed)
	if err != nil {
		return err
	}
	translators, err := translators(ctx, split(mtList))
	if err != nil {
		return err
	}
	opt := bench.Options{
		Recordings: recs, VocabDir: filepath.Join(dir, "vocab"),
		Recognizers: recognizers, Translators: translators, Targets: split(to),
		Vocab: vocab, Pricing: pricing, Log: os.Stderr,
	}

	est, unknown := estimate(opt)
	fmt.Fprintf(os.Stderr, "%d recordings; estimated provider cost ₹%.2f", len(recs), est)
	if len(unknown) > 0 {
		fmt.Fprintf(os.Stderr, " plus unpriced: %s", strings.Join(unknown, ", "))
	}
	fmt.Fprintln(os.Stderr)
	if est > budget && !yes {
		return fmt.Errorf("estimate ₹%.2f is above -budget ₹%.2f; pass -yes to run anyway", est, budget)
	}

	res, err := bench.Run(ctx, opt)
	if err != nil {
		return err
	}
	if out == "" {
		out = filepath.Join(dir, "results", time.Now().Format("20060102-150405"))
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "results.json"), data, 0o644); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(out, "report.md"))
	if err != nil {
		return err
	}
	res.Markdown(f)
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "report:", filepath.Join(out, "report.md"))
	return nil
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func env(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%s is not set", name)
	}
	return v, nil
}

var geminiClient *genai.Client

func gemini(ctx context.Context) (*genai.Client, error) {
	if geminiClient != nil {
		return geminiClient, nil
	}
	key, err := env("GEMINI_API_KEY")
	if err != nil {
		return nil, err
	}
	c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI})
	geminiClient = c
	return c, err
}

func recognizers(ctx context.Context, names []string, speed float64) ([]bench.RecognizerFactory, error) {
	var out []bench.RecognizerFactory
	for _, n := range names {
		switch n {
		case "scripted":
			out = append(out, bench.RecognizerFactory{Name: n, Provider: "scripted", New: func(r bench.Recording, pcm []byte) bench.Recognizer {
				return bench.EngineRecognizer{Label: "scripted", Engine: bench.ScriptedEngine(r.Lang, r.Reference, bench.Seconds(pcm), nil), Speed: 0}
			}})
		case "sarvam":
			key, err := env("SARVAM_API_KEY")
			if err != nil {
				return nil, err
			}
			s := bench.SarvamREST{Key: key, Model: os.Getenv("SARVAM_ASR_MODEL"), Mode: os.Getenv("SARVAM_ASR_MODE")}
			out = append(out, bench.RecognizerFactory{Name: n, Provider: "sarvam", New: func(bench.Recording, []byte) bench.Recognizer { return s }})
		case "gemini":
			c, err := gemini(ctx)
			if err != nil {
				return nil, err
			}
			e := &asr.Gemini{Client: c, Model: os.Getenv("ASR_MODEL")}
			out = append(out, bench.RecognizerFactory{Name: n, Provider: "gemini", New: func(bench.Recording, []byte) bench.Recognizer {
				return bench.EngineRecognizer{Label: "gemini-live", Engine: e, Speed: speed}
			}})
		case "whisper":
			u, err := env("ASR_URL")
			if err != nil {
				return nil, err
			}
			e := &asr.Whisper{URL: u, Model: os.Getenv("ASR_MODEL"), Key: os.Getenv("ASR_API_KEY")}
			out = append(out, bench.RecognizerFactory{Name: n, Provider: "whisper", New: func(bench.Recording, []byte) bench.Recognizer {
				return bench.EngineRecognizer{Label: "whisper", Engine: e, Speed: speed}
			}})
		default:
			return nil, fmt.Errorf("unknown recognizer %q", n)
		}
	}
	return out, nil
}

func translators(ctx context.Context, names []string) ([]bench.NamedTranslator, error) {
	var out []bench.NamedTranslator
	for _, n := range names {
		switch n {
		case "fake":
			out = append(out, bench.NamedTranslator{Name: n, Provider: "fake", T: translate.Fake{}, Glossary: true})
		case "sarvam":
			key, err := env("SARVAM_API_KEY")
			if err != nil {
				return nil, err
			}
			model := os.Getenv("SARVAM_TRANSLATE_MODEL")
			out = append(out, bench.NamedTranslator{Name: "sarvam:" + cmpOr(model, "sarvam-translate:v1"), Provider: "sarvam", T: bench.SarvamTranslator{Key: key, Model: model}})
		case "gemini":
			c, err := gemini(ctx)
			if err != nil {
				return nil, err
			}
			out = append(out, bench.NamedTranslator{Name: "gemini", Provider: "gemini", T: translate.Gemini{Client: c, Model: os.Getenv("TRANSLATE_MODEL")}, Glossary: true})
		case "chat":
			u, err := env("TRANSLATE_URL")
			if err != nil {
				return nil, err
			}
			out = append(out, bench.NamedTranslator{Name: "chat", Provider: "chat", T: translate.Chat{URL: u, Model: os.Getenv("TRANSLATE_MODEL"), Key: os.Getenv("TRANSLATE_API_KEY")}, Glossary: true})
		default:
			return nil, fmt.Errorf("unknown translator %q", n)
		}
	}
	return out, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// estimate prices the run before it starts, from the audio lengths and
// reference character counts. Providers without a rate are listed.
func estimate(o bench.Options) (float64, []string) {
	modes := 2.0
	if o.Vocab != "both" {
		modes = 1
	}
	var seconds float64
	var chars int
	for _, r := range o.Recordings {
		if pcm, err := bench.ReadWAV(r.WAV); err == nil {
			seconds += bench.Seconds(pcm) + 1 // per-request rounding
		}
		chars += len([]rune(r.Reference))
	}
	var total float64
	var unknown []string
	free := map[string]bool{"scripted": true, "fake": true}
	for _, f := range o.Recognizers {
		c, ok := o.Pricing.ASRCost(f.Provider, seconds*modes)
		if !ok && !free[f.Provider] {
			unknown = append(unknown, f.Name)
		}
		total += c
	}
	for _, t := range o.Translators {
		m := modes
		if !t.Glossary {
			m = 1
		}
		c, ok := o.Pricing.TranslateCost(t.Provider, int(float64(chars*len(o.Targets))*m))
		if !ok && !free[t.Provider] {
			unknown = append(unknown, t.Name)
		}
		total += c
	}
	return total, unknown
}
