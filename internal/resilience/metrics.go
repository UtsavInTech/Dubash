// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package resilience exposes process-local provider failover counters.
package resilience

import (
	"fmt"
	"io"
	"sync/atomic"
)

var ASRSwitches atomic.Int64
var TranslationFallbacks atomic.Int64
var TranslationFailures atomic.Int64

func Write(w io.Writer) {
	for _, m := range []struct {
		name, help string
		value      int64
	}{
		{"dubash_asr_failovers_total", "Recognition sessions switched to the backup provider.", ASRSwitches.Load()},
		{"dubash_translation_fallbacks_total", "Translation requests sent to the backup provider.", TranslationFallbacks.Load()},
		{"dubash_translation_failures_total", "Translation requests for which both providers failed.", TranslationFailures.Load()},
	} {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", m.name, m.help, m.name, m.name, m.value)
	}
}
