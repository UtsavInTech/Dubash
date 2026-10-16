// Copyright 2026 the Dubash contributors. Apache-2.0.

package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/translate"
)

func TestFakeModeCannotUsePaidProviders(t *testing.T) {
	t.Setenv("SARVAM_API_KEY", "must-not-be-used")
	t.Setenv("GEMINI_API_KEY", "must-not-be-used")
	t.Setenv("CORRECTION", "model")
	t.Setenv("LEXICON", "")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine, translator, _, err := backends(context.Background(), log, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := engine.(asr.Fake); !ok {
		t.Fatalf("fake ASR: %T", engine)
	}
	if _, ok := translator.(translate.Fake); !ok {
		t.Fatalf("fake translator: %T", translator)
	}
	corr, err := newCorrector(log, true, &translate.Meter{})
	if err != nil || corr == nil || corr.Model != nil {
		t.Fatalf("fake correction may call a provider: %+v %v", corr, err)
	}
}
