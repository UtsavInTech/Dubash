// Copyright 2026 the Dubash contributors. Apache-2.0.

package bench

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/UtsavInTech/dubash/internal/translate"
)

// RecognizerFactory builds the recognizer for one recording (the scripted
// fake needs the reference; real providers ignore it).
type RecognizerFactory struct {
	Name string
	// Provider keys the pricing file (sarvam, gemini…).
	Provider string
	New      func(r Recording, pcm []byte) Recognizer
}

// NamedTranslator is a translation provider under test.
type NamedTranslator struct {
	Name     string
	Provider string
	T        translate.Translator
	// Glossary says whether T honours its glossary argument. Translators
	// that ignore it run only without one: a second run would cost the
	// same and change nothing.
	Glossary bool
}

// Options configure a benchmark run.
type Options struct {
	Recordings  []Recording
	VocabDir    string
	Recognizers []RecognizerFactory
	Translators []NamedTranslator
	Targets     []string
	// Vocab is "both" (baseline and with vocabulary), "on" or "off".
	Vocab   string
	Pricing Pricing
	Log     io.Writer
}

// ASRResult is one recording through one recognizer, with or without
// vocabulary.
type ASRResult struct {
	Recording  string        `json:"recording"`
	Lang       string        `json:"lang"`
	Tags       []string      `json:"tags,omitempty"`
	Recognizer string        `json:"recognizer"`
	WithVocab  bool          `json:"withVocab"`
	Hints      int           `json:"hints"`
	Audio      float64       `json:"audioSeconds"`
	WER        float64       `json:"wer"`
	CER        float64       `json:"cer"`
	Terms      TermScore     `json:"terms"`
	Misheard   int           `json:"misheard"` // known mis-hearings present in the output
	Numbers    NumberScore   `json:"numbers"`
	Negations  NegationScore `json:"negations"`
	Rec        Recognition   `json:"recognition"`
	CostINR    *float64      `json:"costInr,omitempty"`
	Err        string        `json:"error,omitempty"`
}

// MTResult is one recording's reference transcript translated into one
// language by one translator, with or without the glossary.
type MTResult struct {
	Recording    string          `json:"recording"`
	Translator   string          `json:"translator"`
	Src          string          `json:"src"`
	Dst          string          `json:"dst"`
	WithGlossary bool            `json:"withGlossary"`
	Audio        float64         `json:"audioSeconds"`
	Sentences    int             `json:"sentences"`
	Chars        int             `json:"chars"`
	Latencies    []time.Duration `json:"latenciesNs"`
	ChrF         *float64        `json:"chrf,omitempty"`
	TermsWanted  int             `json:"termsWanted"`
	TermsKept    int             `json:"termsKept"`
	TermsMissing []string        `json:"termsMissing,omitempty"`
	Output       string          `json:"output"`
	CostINR      *float64        `json:"costInr,omitempty"`
	Errs         []string        `json:"errors,omitempty"`
}

// Results of a whole run.
type Results struct {
	Started time.Time   `json:"started"`
	Pricing string      `json:"pricingChecked"`
	ASR     []ASRResult `json:"asr"`
	MT      []MTResult  `json:"mt"`
}

func (o Options) vocabModes() []bool {
	switch o.Vocab {
	case "on":
		return []bool{true}
	case "off":
		return []bool{false}
	}
	return []bool{false, true}
}

func (o Options) logf(format string, args ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, format+"\n", args...)
	}
}

// Run benchmarks every recording through every recognizer and
// translator. Provider failures are recorded in the results, not
// returned: one failing provider does not stop the others.
func Run(ctx context.Context, o Options) (Results, error) {
	res := Results{Started: time.Now().UTC(), Pricing: o.Pricing.Checked}
	for _, r := range o.Recordings {
		pcm, err := ReadWAV(r.WAV)
		if err != nil {
			return res, fmt.Errorf("recording %s: %w", r.Name, err)
		}
		vocab, err := LoadVocabulary(o.VocabDir, r.Vocabulary)
		if err != nil {
			return res, err
		}
		for _, f := range o.Recognizers {
			for _, with := range o.vocabModes() {
				if with && len(vocab.Terms) == 0 {
					continue // nothing to compare against the baseline
				}
				res.ASR = append(res.ASR, o.recognize(ctx, f, r, pcm, vocab, with))
			}
		}
		for _, t := range o.Translators {
			for _, dst := range o.Targets {
				if dst == r.Lang {
					continue
				}
				for _, with := range o.vocabModes() {
					if with && (len(vocab.Terms) == 0 || !t.Glossary) {
						continue
					}
					res.MT = append(res.MT, o.translate(ctx, t, r, Seconds(pcm), vocab, dst, with))
				}
			}
		}
	}
	return res, nil
}

func (o Options) recognize(ctx context.Context, f RecognizerFactory, r Recording, pcm []byte, vocab Vocabulary, with bool) ASRResult {
	var hints []string
	if with {
		hints = vocab.Hints()
	}
	rz := f.New(r, pcm)
	o.logf("asr  %-24s %-28s vocab=%-5v (%.0f s audio)", r.Name, rz.Name(), with, Seconds(pcm))
	out := ASRResult{Recording: r.Name, Lang: r.Lang, Tags: r.Tags, Recognizer: rz.Name(), WithVocab: with, Hints: len(hints), Audio: Seconds(pcm)}
	rec, err := rz.Recognize(ctx, pcm, r.Lang, hints)
	out.Rec = rec
	if err != nil {
		out.Err = fmtErr(err)
		o.logf("     error: %s", out.Err)
		return out
	}
	out.WER = WER(r.Reference, rec.Text)
	out.CER = CER(r.Reference, rec.Text)
	out.Terms = ScoreTerms(r.Reference, rec.Text, vocab.Spellings())
	for _, t := range vocab.Terms {
		out.Misheard += countAny(Tokens(rec.Text), t.Misheard)
	}
	out.Numbers = ScoreNumbers(r.Reference, rec.Text)
	out.Negations = ScoreNegations(r.Reference, rec.Text, r.Lang)
	if c, ok := o.Pricing.ASRCost(f.Provider, rec.BilledSeconds); ok {
		out.CostINR = &c
	}
	o.logf("     WER %.3f  CER %.3f  terms %d/%d  inserted %d", out.WER, out.CER, out.Terms.Found, out.Terms.Expected, out.Terms.Inserted)
	return out
}

func (o Options) translate(ctx context.Context, t NamedTranslator, r Recording, audio float64, vocab Vocabulary, dst string, with bool) MTResult {
	var glossary []string
	if with {
		glossary = vocab.Keep()
	}
	o.logf("mt   %-24s %-28s %s→%s glossary=%v", r.Name, t.Name, r.Lang, dst, with)
	out := MTResult{Recording: r.Name, Translator: t.Name, Src: r.Lang, Dst: dst, WithGlossary: with, Audio: audio}
	var lines []string
	for _, s := range Sentences(r.Reference) {
		out.Sentences++
		out.Chars += len([]rune(s))
		t0 := time.Now()
		tr, err := t.T.Translate(ctx, s, r.Lang, dst, glossary)
		out.Latencies = append(out.Latencies, time.Since(t0))
		if err != nil {
			out.Errs = append(out.Errs, fmtErr(err))
			continue
		}
		lines = append(lines, tr)
		wanted, kept, missing := TranslationTerms(s, tr, dst, vocab)
		out.TermsWanted += wanted
		out.TermsKept += kept
		out.TermsMissing = append(out.TermsMissing, missing...)
	}
	out.Output = strings.Join(lines, " ")
	if ref := r.Translations[dst]; ref != "" && len(lines) > 0 {
		c := ChrF(ref, out.Output)
		out.ChrF = &c
	}
	if c, ok := o.Pricing.TranslateCost(t.Provider, out.Chars); ok {
		out.CostINR = &c
	}
	return out
}

// TranslationTerms checks one translated sentence against the vocabulary:
// a "keep" term spoken in src must appear unchanged in out, and a term
// with an approved translation into dst must appear as that translation.
func TranslationTerms(src, out, dst string, v Vocabulary) (wanted, kept int, missing []string) {
	st, ot := Tokens(src), Tokens(out)
	for _, t := range v.Terms {
		spoken := countAny(st, append([]string{t.Term}, t.Aliases...))
		if spoken == 0 {
			continue
		}
		var want string
		switch {
		case t.Translations[dst] != "":
			want = t.Translations[dst]
		case t.Keep:
			want = t.Term
		default:
			continue
		}
		wanted += spoken
		got := min(spoken, count(ot, Tokens(want)))
		kept += got
		if got < spoken {
			missing = append(missing, t.Term)
		}
	}
	return wanted, kept, missing
}

// Percentile returns the p-th percentile (0–100) of ds, nearest rank.
func Percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	i := int(float64(len(s))*p/100+0.5) - 1
	return s[max(0, min(i, len(s)-1))]
}
