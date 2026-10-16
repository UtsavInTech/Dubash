// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/UtsavInTech/dubash/internal/resilience"
)

// Failover bounds the primary request and opens a circuit after repeated
// failures. During Cooldown requests go straight to the independent backup.
// A single half-open probe is allowed after the cooldown.
// It deliberately does not implement Batcher: each line has its own deadline.
type Failover struct {
	Primary, Backup   Translator
	Timeout, Cooldown time.Duration
	Threshold         int
	mu                sync.Mutex
	failures          int
	until             time.Time
	probing           bool
}

func (f *Failover) Translate(ctx context.Context, text, src, dst string, g Glossary) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	timeout, cooldown, threshold := f.Timeout, f.Cooldown, f.Threshold
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	if threshold <= 0 {
		threshold = 3
	}
	f.mu.Lock()
	usePrimary := f.until.IsZero() || (!time.Now().Before(f.until) && !f.probing)
	probe := usePrimary && !f.until.IsZero()
	if probe {
		f.probing = true
	}
	f.mu.Unlock()
	var primaryErr error
	if usePrimary {
		pctx, cancel := context.WithTimeout(ctx, timeout)
		out, err := f.Primary.Translate(pctx, text, src, dst, g)
		cancel()
		if err == nil && strings.TrimSpace(out) == "" {
			err = errors.New("primary returned an empty translation")
		}
		if ctx.Err() != nil {
			if probe {
				f.mu.Lock()
				f.probing = false
				f.mu.Unlock()
			}
			return "", ctx.Err()
		}
		f.mu.Lock()
		if probe {
			f.probing = false
		}
		if err == nil {
			f.failures, f.until = 0, time.Time{}
		} else {
			f.failures++
			if f.failures >= threshold || probe {
				f.until = time.Now().Add(cooldown)
			}
		}
		f.mu.Unlock()
		if err == nil {
			return out, nil
		}
		primaryErr = err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	resilience.TranslationFallbacks.Add(1)
	out, err := f.Backup.Translate(ctx, text, src, dst, g)
	if err == nil && strings.TrimSpace(out) == "" {
		err = errors.New("backup returned an empty translation")
	}
	if err != nil {
		resilience.TranslationFailures.Add(1)
		return "", errors.Join(primaryErr, err)
	}
	return out, nil
}
