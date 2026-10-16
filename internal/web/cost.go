// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"math"

	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/translate"
)

// Prices are the providers' rates, in rupees (docs.sarvam.ai pricing,
// checked 2026-10-16). Zero rates mean free (the fake engines).
type Prices struct {
	SpeechPerHour float64 // recognition, per audio hour: Sarvam ₹30
	// Translation with a chat model, per million tokens (Sarvam 105B: ₹15
	// in, ₹60 out), measured by Meter.
	PromptPerM, CompletionPerM float64
	// Translation billed per character (Sarvam Translate: ₹20 per 10,000),
	// used when there is no token meter.
	Per10kChars float64
}

// SarvamPrices are Sarvam's rates for its speech, chat and translate APIs.
var SarvamPrices = Prices{SpeechPerHour: 30, PromptPerM: 15, CompletionPerM: 60, Per10kChars: 20}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

func tokenCost(p Prices, m *translate.Meter) float64 {
	if m == nil {
		return 0
	}
	return float64(m.PromptTokens.Load())*p.PromptPerM/1e6 + float64(m.CompletionTokens.Load())*p.CompletionPerM/1e6
}

// costs fills in each status's Cost. With a token meter, the tokens'
// price is shared out over rooms and languages by characters translated;
// the correction model's (corr) over rooms by audio recognized.
func costs(statuses []room.Status, p Prices, m, corr *translate.Meter) {
	var chars int64
	for _, st := range statuses {
		for _, n := range st.TranslatedChars {
			chars += n
		}
	}
	perChar := p.Per10kChars / 10000
	if m != nil {
		perChar = 0
		if chars > 0 {
			perChar = tokenCost(p, m) / float64(chars)
		}
	}
	var audio float64
	for _, st := range statuses {
		audio += st.AudioTotalSeconds
	}
	perSecond := 0.0
	if audio > 0 {
		perSecond = tokenCost(p, corr) / audio
	}
	for i := range statuses {
		st := &statuses[i]
		c := &room.Cost{Speech: round2(st.AudioTotalSeconds / 3600 * p.SpeechPerHour), Translation: map[string]float64{}}
		corrCost := st.AudioTotalSeconds * perSecond
		c.Correction = round2(corrCost)
		total := c.Speech + corrCost
		for l, n := range st.TranslatedChars {
			c.Translation[l] = round2(float64(n) * perChar)
			total += float64(n) * perChar
		}
		c.Total = round2(total)
		if st.AudioTotalSeconds > 0 {
			c.PerHour = round2(total / (st.AudioTotalSeconds / 3600))
		}
		st.Cost = c
	}
}
