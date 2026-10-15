// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package bench measures speech recognition and translation providers on
// recorded audio: word and character error rate, vocabulary term
// accuracy, numbers and negations, latency and cost, each with and
// without a vocabulary list. It backs cmd/dubash-bench.
package bench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Recording is one audio file and what was really said in it, read from
// NAME.yaml next to NAME.wav.
type Recording struct {
	Name string `yaml:"-"`
	WAV  string `yaml:"-"`
	// Lang is the main spoken language (en, hi, te, kn, ta…). Code-switched
	// Hindi-English speech is lang hi with the tag code-switch.
	Lang string `yaml:"lang"`
	// Reference is the exact transcript, in the script the speaker's
	// language is normally written in, with figures written as digits.
	Reference string `yaml:"reference"`
	// Vocabulary names a file in the vocab directory (without .yaml).
	Vocabulary string `yaml:"vocabulary"`
	// Tags describe the conditions: accent, code-switch, noise, numbers,
	// names, technical, synthetic…
	Tags []string `yaml:"tags"`
	// Translations are optional reference translations, by language code,
	// for the chrF score.
	Translations map[string]string `yaml:"translations"`
	// Notes are free text for whoever reads the report.
	Notes string `yaml:"notes"`
}

// Term is one vocabulary entry: the shape the Dictation Bank will take,
// kept small for the benchmark.
type Term struct {
	Term string `yaml:"term"`
	// Aliases are other correct spellings that count as the term.
	Aliases []string `yaml:"aliases"`
	// Misheard are known wrong recognitions. They are not sent as hints;
	// the report counts them to show what the bank should fix.
	Misheard []string `yaml:"misheard"`
	// Keep means never translate: the term must appear unchanged in every
	// translation.
	Keep bool `yaml:"keep"`
	// Translations are the approved renderings per target language.
	Translations map[string]string `yaml:"translations"`
}

// Vocabulary is a named list of terms.
type Vocabulary struct {
	Name  string `yaml:"-"`
	Terms []Term `yaml:"terms"`
}

// Hints are the strings sent to a recognizer as phrase hints: each term
// and its aliases.
func (v Vocabulary) Hints() []string {
	var out []string
	for _, t := range v.Terms {
		for _, s := range append([]string{t.Term}, t.Aliases...) {
			if s != "" && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// Spellings lists, per term, the spellings that count as recognizing it.
func (v Vocabulary) Spellings() [][]string {
	out := make([][]string, 0, len(v.Terms))
	for _, t := range v.Terms {
		out = append(out, append([]string{t.Term}, t.Aliases...))
	}
	return out
}

// Keep lists the terms that must never be translated.
func (v Vocabulary) Keep() []string {
	var out []string
	for _, t := range v.Terms {
		if t.Keep {
			out = append(out, t.Term)
		}
	}
	return out
}

// LoadRecordings reads every NAME.yaml in dir that has a NAME.wav beside
// it, sorted by name. A YAML file without its audio is an error, so a
// misnamed file is not silently skipped.
func LoadRecordings(dir string) ([]Recording, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	var out []Recording
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".yaml")
		wav := filepath.Join(dir, name+".wav")
		if _, err := os.Stat(wav); err != nil {
			return nil, fmt.Errorf("recording %s: %s.wav is missing next to %s.yaml", name, name, name)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var r Recording
		if err := yaml.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("recording %s: %w", name, err)
		}
		if r.Lang == "" || strings.TrimSpace(r.Reference) == "" {
			return nil, fmt.Errorf("recording %s: lang and reference are required", name)
		}
		r.Name, r.WAV = name, wav
		out = append(out, r)
	}
	return out, nil
}

// LoadVocabulary reads dir/NAME.yaml. An empty name is an empty list.
func LoadVocabulary(dir, name string) (Vocabulary, error) {
	if name == "" {
		return Vocabulary{}, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, name+".yaml"))
	if err != nil {
		return Vocabulary{}, fmt.Errorf("vocabulary %s: %w", name, err)
	}
	var v Vocabulary
	if err := yaml.Unmarshal(data, &v); err != nil {
		return Vocabulary{}, fmt.Errorf("vocabulary %s: %w", name, err)
	}
	for i, t := range v.Terms {
		if strings.TrimSpace(t.Term) == "" {
			return Vocabulary{}, fmt.Errorf("vocabulary %s: term %d is empty", name, i)
		}
	}
	v.Name = name
	return v, nil
}

// Pricing holds provider rates in rupees. A provider absent from the
// file has an unknown cost, reported as such rather than as zero.
type Pricing struct {
	Checked string `yaml:"checked"` // when and where the rates were read
	ASR     map[string]struct {
		PerHour float64 `yaml:"per_hour"`
	} `yaml:"asr"`
	Translate map[string]struct {
		Per10kChars float64 `yaml:"per_10k_chars"`
	} `yaml:"translate"`
}

// LoadPricing reads the pricing file; a missing file means every cost is
// unknown.
func LoadPricing(path string) (Pricing, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Pricing{}, nil
	}
	if err != nil {
		return Pricing{}, err
	}
	var p Pricing
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Pricing{}, fmt.Errorf("pricing: %w", err)
	}
	return p, nil
}

// ASRCost is the rupee cost of seconds of audio with provider, and
// whether the rate is known.
func (p Pricing) ASRCost(provider string, seconds float64) (float64, bool) {
	r, ok := p.ASR[provider]
	return r.PerHour * seconds / 3600, ok
}

// TranslateCost is the rupee cost of translating chars characters.
func (p Pricing) TranslateCost(provider string, chars int) (float64, bool) {
	r, ok := p.Translate[provider]
	return r.Per10kChars * float64(chars) / 10000, ok
}
