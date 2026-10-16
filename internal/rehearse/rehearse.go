// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package rehearse runs a short test recording through an event's
// pipeline before the event: recognition without and with the Dictation
// Bank's hints, the correction stage with the script, and translation,
// scored against what was really said when the admin provides it.
package rehearse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/bench"
	"github.com/UtsavInTech/dubash/internal/correct"
	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/script"
)

// MaxSeconds bounds a rehearsal recording: it is billed twice.
const MaxSeconds = 180

// Score compares a step's text with the reference.
type Score struct {
	WER        float64 `json:"wer"`
	CER        float64 `json:"cer"`
	TermsFound int     `json:"termsFound"`
	Terms      int     `json:"terms"`
	Inserted   int     `json:"inserted"`
	Numbers    string  `json:"numbers"`
	Negations  string  `json:"negations"`
}

// Step is one stage's output.
type Step struct {
	Name  string         `json:"name"`
	Text  string         `json:"text"`
	Score *Score         `json:"score,omitempty"`
	Fixes []correct.Edit `json:"fixes,omitempty"`
	Took  time.Duration  `json:"tookMs"`
}

// Report is a rehearsal's result.
type Report struct {
	Lang         string            `json:"lang"`
	Seconds      float64           `json:"seconds"`
	Version      int               `json:"version"`
	Hints        int               `json:"hints"`
	Steps        []Step            `json:"steps"`
	Translations map[string]string `json:"translations"`
	Errors       []string          `json:"errors,omitempty"`
}

// Run rehearses pcm (PCM16 mono 16 kHz) in the room's context.
func Run(ctx context.Context, rm *room.Room, pcm []byte, lang, reference string) (Report, error) {
	secs := bench.Seconds(pcm)
	if secs > MaxSeconds {
		return Report{}, fmt.Errorf("the recording is %.0f s long; rehearse with %d s at most", secs, MaxSeconds)
	}
	if secs < 1 {
		return Report{}, errors.New("the recording is empty")
	}
	deps, vocab := rm.Deps(), rm.Context()
	rep := Report{Lang: lang, Seconds: secs, Version: vocab.Version.Number, Translations: map[string]string{}}
	hints := vocab.Hints(lang)
	rep.Hints = len(hints)
	score := func(text string) *Score {
		if strings.TrimSpace(reference) == "" {
			return nil
		}
		var spellings [][]string
		for _, e := range vocab.Entries() {
			spellings = append(spellings, e.Spellings())
		}
		t := bench.ScoreTerms(reference, text, spellings)
		n := bench.ScoreNumbers(reference, text)
		g := bench.ScoreNegations(reference, text, lang)
		return &Score{
			WER: bench.WER(reference, text), CER: bench.CER(reference, text),
			TermsFound: t.Found, Terms: t.Expected, Inserted: t.Inserted,
			Numbers: fmt.Sprintf("%d/%d", n.Correct, n.Expected), Negations: fmt.Sprintf("%d/%d", g.Found, g.Expected),
		}
	}
	recognize := func(name string, vocabulary []string) (string, error) {
		start := time.Now()
		rz := bench.EngineRecognizer{Label: name, Engine: modeEngine{deps.ASR, rm.Config().ASRMode}, Speed: 0, Drain: 8 * time.Second}
		rec, err := rz.Recognize(ctx, pcm, lang, vocabulary)
		if err != nil {
			return "", err
		}
		rep.Steps = append(rep.Steps, Step{Name: name, Text: rec.Text, Score: score(rec.Text), Took: time.Since(start)})
		return rec.Text, nil
	}
	if _, err := recognize("Recognition without the bank", nil); err != nil {
		return rep, err
	}
	withBank, err := recognize("Recognition with the bank's hints", hints)
	if err != nil {
		return rep, err
	}

	// Correction, line by line, following the script.
	corrected := withBank
	if c := deps.Corrector; c != nil {
		start := time.Now()
		al := script.NewAligner(vocab.Index)
		var lines, recent []string
		var fixes []correct.Edit
		for _, line := range bench.Sentences(withBank) {
			pos, conf := al.Observe(line)
			res := c.Correct(ctx, correct.Input{Text: line, Lang: lang, Entries: vocab.Entries(), Script: al.Window(pos), ScriptConf: conf, Previous: recent})
			lines = append(lines, res.Text)
			fixes = append(fixes, res.Edits...)
			recent = append(recent, res.Text)
		}
		corrected = strings.Join(lines, " ")
		rep.Steps = append(rep.Steps, Step{Name: "After correction", Text: corrected, Score: score(corrected), Fixes: fixes, Took: time.Since(start)})
	}

	// Translation into the event's other languages.
	for _, dst := range rm.Config().Languages() {
		if dst == lang {
			continue
		}
		out, err := deps.Translator.Translate(ctx, corrected, lang, dst, vocab.Glossary(lang, dst))
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("translation into %s: %v", dst, err))
			continue
		}
		rep.Translations[dst] = out
	}
	return rep, nil
}

// modeEngine passes the room's recognition mode to the engine.
type modeEngine struct {
	asr.Engine
	mode string
}

func (m modeEngine) Run(ctx context.Context, cfg asr.Config, audio <-chan []byte, events chan<- asr.Event) error {
	cfg.Mode = m.mode
	return m.Engine.Run(ctx, cfg, audio, events)
}
