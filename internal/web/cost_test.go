// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"testing"

	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/translate"
)

func TestCosts(t *testing.T) {
	// One hour of speech; 50,000 characters into Telugu, 25,000 into
	// Kannada (another room).
	st := []room.Status{
		{AudioTotalSeconds: 3600, TranslatedChars: map[string]int64{"te": 50000}},
		{AudioTotalSeconds: 1800, TranslatedChars: map[string]int64{"kn": 25000}},
	}
	// Per character (the translate API): ₹20 per 10,000.
	costs(st, SarvamPrices, nil)
	if c := st[0].Cost; c.Speech != 30 || c.Translation["te"] != 100 || c.Total != 130 || c.PerHour != 130 {
		t.Errorf("per character: %+v", c)
	}
	// Per token (the chat model): 1M prompt and 0.25M completion tokens
	// cost ₹15 + ₹15 = ₹30, shared 2:1 by characters.
	m := &translate.Meter{}
	m.PromptTokens.Store(1_000_000)
	m.CompletionTokens.Store(250_000)
	costs(st, SarvamPrices, m)
	if c := st[0].Cost; c.Translation["te"] != 20 || c.Total != 50 {
		t.Errorf("per token, room 1: %+v", c)
	}
	if c := st[1].Cost; c.Speech != 15 || c.Translation["kn"] != 10 || c.PerHour != 50 {
		t.Errorf("per token, room 2: %+v", c)
	}
	costs(st, Prices{}, nil)
	if st[0].Cost.Total != 0 {
		t.Errorf("free engines cost %v", st[0].Cost.Total)
	}
}
