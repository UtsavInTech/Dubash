// Copyright 2026 the Dubash contributors. Apache-2.0.

package bench

import (
	"fmt"
	"io"
	"strings"
	"time"
)

func ms(d time.Duration) string {
	if d == 0 {
		return "–"
	}
	return fmt.Sprintf("%d", d.Milliseconds())
}

func inr(c *float64) string {
	if c == nil {
		return "?"
	}
	return fmt.Sprintf("₹%.3f", *c)
}

func pct(f float64) string { return fmt.Sprintf("%.1f%%", 100*f) }

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// Markdown writes the human report: per-configuration summaries first
// (with vocabulary against baseline), then every run.
func (res Results) Markdown(w io.Writer) {
	fmt.Fprintf(w, "# Benchmark %s\n\n", res.Started.Format(time.RFC3339))
	if res.Pricing != "" {
		fmt.Fprintf(w, "Prices: %s Verify before quoting.\n\n", strings.TrimSuffix(res.Pricing, ".")+".")
	}
	res.asrSummary(w)
	res.mtSummary(w)

	fmt.Fprintln(w, "## Recognition, every run")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Recording | Tags | Recognizer | Vocab | WER | CER | Terms found | Inserted | Mis-heard | Numbers | Negations ref/hyp | First final ms | Lag p50/p95 ms | Cost | Error |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
	for _, r := range res.ASR {
		fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %d/%d | %d | %d | %d/%d | %d/%d | %s | %s/%s | %s | %s |\n",
			r.Recording, strings.Join(r.Tags, " "), r.Recognizer, onOff(r.WithVocab), pct(r.WER), pct(r.CER),
			r.Terms.Found, r.Terms.Expected, r.Terms.Inserted, r.Misheard, r.Numbers.Correct, r.Numbers.Expected,
			r.Negations.Expected, r.Negations.Found, ms(r.Rec.FirstFinal),
			ms(Percentile(r.Rec.Lags, 50)), ms(Percentile(r.Rec.Lags, 95)), inr(r.CostINR), cell(r.Err))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Translation, every run")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Recording | Translator | Pair | Glossary | Lines | Chars | chrF | Terms kept | Latency p50/p95 ms | Cost | Errors |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|")
	for _, m := range res.MT {
		chrf := "–"
		if m.ChrF != nil {
			chrf = fmt.Sprintf("%.1f", *m.ChrF)
		}
		fmt.Fprintf(w, "| %s | %s | %s→%s | %s | %d | %d | %s | %d/%d | %s/%s | %s | %s |\n",
			m.Recording, m.Translator, m.Src, m.Dst, onOff(m.WithGlossary), m.Sentences, m.Chars, chrf,
			m.TermsKept, m.TermsWanted, ms(Percentile(m.Latencies, 50)), ms(Percentile(m.Latencies, 95)),
			inr(m.CostINR), cell(strings.Join(m.Errs, "; ")))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Outputs")
	fmt.Fprintln(w)
	for _, r := range res.ASR {
		fmt.Fprintf(w, "- **%s · %s · vocab %s**: %s\n", r.Recording, r.Recognizer, onOff(r.WithVocab), cell(r.Rec.Text))
	}
	for _, m := range res.MT {
		fmt.Fprintf(w, "- **%s · %s · %s→%s · glossary %s**: %s\n", m.Recording, m.Translator, m.Src, m.Dst, onOff(m.WithGlossary), cell(m.Output))
	}
}

// cell keeps text inside one table cell.
func cell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s)
}

func (res Results) asrSummary(w io.Writer) {
	type key struct {
		rec  string
		with bool
	}
	type agg struct {
		n, terms, found, inserted, nums, numsOK, neg, negOK int
		wer, cer, audio, cost                               float64
		costKnown                                           bool
		lags                                                []time.Duration
	}
	var order []key
	m := map[key]*agg{}
	for _, r := range res.ASR {
		if r.Err != "" {
			continue
		}
		k := key{r.Recognizer, r.WithVocab}
		a, ok := m[k]
		if !ok {
			a = &agg{costKnown: true}
			m[k] = a
			order = append(order, k)
		}
		a.n++
		a.wer += r.WER
		a.cer += r.CER
		a.terms += r.Terms.Expected
		a.found += r.Terms.Found
		a.inserted += r.Terms.Inserted
		a.nums += r.Numbers.Expected
		a.numsOK += r.Numbers.Correct
		a.neg++
		if r.Negations.Agrees() {
			a.negOK++
		}
		a.audio += r.Audio
		a.lags = append(a.lags, r.Rec.Lags...)
		if r.CostINR == nil {
			a.costKnown = false
		} else {
			a.cost += *r.CostINR
		}
	}
	fmt.Fprintln(w, "## Recognition summary")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Means over recordings. Compare each recognizer's vocab off and on rows: same audio, only the hints differ.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Recognizer | Vocab | Runs | WER | CER | Term recall | Inserted terms | Numbers right | Negations agree | Lag p50/p95 ms | ₹ per audio hour |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|---|---|")
	for _, k := range order {
		a := m[k]
		perHour := "?"
		if a.costKnown && a.audio > 0 {
			perHour = fmt.Sprintf("₹%.2f", a.cost/a.audio*3600)
		}
		fmt.Fprintf(w, "| %s | %s | %d | %s | %s | %s | %d | %s | %d/%d | %s/%s | %s |\n",
			k.rec, onOff(k.with), a.n, pct(a.wer/float64(a.n)), pct(a.cer/float64(a.n)),
			ratio(a.found, a.terms), a.inserted, ratio(a.numsOK, a.nums), a.negOK, a.neg,
			ms(Percentile(a.lags, 50)), ms(Percentile(a.lags, 95)), perHour)
	}
	fmt.Fprintln(w)
}

func (res Results) mtSummary(w io.Writer) {
	type key struct {
		tr, dst string
		with    bool
	}
	type agg struct {
		n, wanted, kept, chars, chrfN, errs int
		chrf, audio, cost                   float64
		costKnown                           bool
		lat                                 []time.Duration
	}
	var order []key
	m := map[key]*agg{}
	for _, r := range res.MT {
		k := key{r.Translator, r.Dst, r.WithGlossary}
		a, ok := m[k]
		if !ok {
			a = &agg{costKnown: true}
			m[k] = a
			order = append(order, k)
		}
		a.n++
		a.wanted += r.TermsWanted
		a.kept += r.TermsKept
		a.chars += r.Chars
		a.audio += r.Audio
		a.errs += len(r.Errs)
		a.lat = append(a.lat, r.Latencies...)
		if r.ChrF != nil {
			a.chrf += *r.ChrF
			a.chrfN++
		}
		if r.CostINR == nil {
			a.costKnown = false
		} else {
			a.cost += *r.CostINR
		}
	}
	fmt.Fprintln(w, "## Translation summary")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Input is the reference transcript, sentence by sentence, so translation is measured apart from recognition errors. ₹ per audio hour is per target language: the speech's characters per hour times the rate.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Translator | Target | Glossary | Recordings | chrF | Terms kept | Latency p50/p95 ms | Errors | ₹ per audio hour |")
	fmt.Fprintln(w, "|---|---|---|---|---|---|---|---|---|")
	for _, k := range order {
		a := m[k]
		chrf := "–"
		if a.chrfN > 0 {
			chrf = fmt.Sprintf("%.1f", a.chrf/float64(a.chrfN))
		}
		perHour := "?"
		if a.costKnown && a.audio > 0 {
			perHour = fmt.Sprintf("₹%.2f", a.cost/a.audio*3600)
		}
		fmt.Fprintf(w, "| %s | %s | %s | %d | %s | %s | %s/%s | %d | %s |\n",
			k.tr, k.dst, onOff(k.with), a.n, chrf, ratio(a.kept, a.wanted),
			ms(Percentile(a.lat, 50)), ms(Percentile(a.lat, 95)), a.errs, perHour)
	}
	fmt.Fprintln(w)
}

func ratio(a, b int) string {
	if b == 0 {
		return "–"
	}
	return fmt.Sprintf("%d/%d (%.0f%%)", a, b, 100*float64(a)/float64(b))
}
