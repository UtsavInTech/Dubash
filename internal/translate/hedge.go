package translate

import (
	"context"
	"time"
)

// DefaultHedgeAfter is how long Hedged waits before racing a second
// request: most translations answer within a second, but a few take
// several.
const DefaultHedgeAfter = 1500 * time.Millisecond

// Hedged cuts tail latency: when Inner has not answered after After
// (DefaultHedgeAfter if zero), it sends the same request again and
// returns whichever answers first, cancelling the other.
type Hedged struct {
	Inner Translator
	After time.Duration
}

// Translate implements Translator.
func (h Hedged) Translate(ctx context.Context, text, src, dst string, g Glossary) (string, error) {
	after := h.After
	if after <= 0 {
		after = DefaultHedgeAfter
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // stops the request that lost
	type result struct {
		text string
		err  error
	}
	results := make(chan result, 2)
	call := func() {
		out, err := h.Inner.Translate(ctx, text, src, dst, g)
		results <- result{out, err}
	}
	go call()
	running := 1
	hedge := time.NewTimer(after)
	defer hedge.Stop()
	var firstErr error
	for {
		select {
		case <-hedge.C:
			go call()
			running++
		case r := <-results:
			running--
			if r.err == nil {
				return r.text, nil
			}
			if firstErr == nil {
				firstErr = r.err
			}
			if running == 0 {
				return "", firstErr // an early failure is not retried
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
