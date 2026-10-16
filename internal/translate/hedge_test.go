package translate

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// slow answers each call after the next delay in delays (the last one
// repeats), or with err, and records how calls ended.
type slow struct {
	delays    []time.Duration
	err       error
	calls     atomic.Int32
	cancelled atomic.Int32
}

func (s *slow) Translate(ctx context.Context, text, _, dst string, _ Glossary) (string, error) {
	n := int(s.calls.Add(1)) - 1
	d := s.delays[min(n, len(s.delays)-1)]
	select {
	case <-time.After(d):
	case <-ctx.Done():
		s.cancelled.Add(1)
		return "", ctx.Err()
	}
	if s.err != nil {
		return "", s.err
	}
	return dst + ":" + text + ":" + string(rune('a'+n)), nil
}

func TestHedgedFastAnswerMakesOneCall(t *testing.T) {
	inner := &slow{delays: []time.Duration{10 * time.Millisecond}}
	got, err := Hedged{Inner: inner, After: 200 * time.Millisecond}.Translate(context.Background(), "hi", "en", "es", Glossary{})
	if err != nil || got != "es:hi:a" {
		t.Fatalf("got %q, %v", got, err)
	}
	time.Sleep(250 * time.Millisecond)
	if n := inner.calls.Load(); n != 1 {
		t.Fatalf("%d calls, want 1", n)
	}
}

func TestHedgedSlowAnswerIsRaced(t *testing.T) {
	inner := &slow{delays: []time.Duration{time.Hour, 10 * time.Millisecond}}
	start := time.Now()
	got, err := Hedged{Inner: inner, After: 50 * time.Millisecond}.Translate(context.Background(), "hi", "en", "es", Glossary{})
	if err != nil || got != "es:hi:b" {
		t.Fatalf("got %q, %v", got, err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v", d)
	}
	deadline := time.Now().Add(time.Second)
	for inner.cancelled.Load() != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the slow request was not cancelled")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHedgedFirstAnswerStillWinsAfterHedging(t *testing.T) {
	inner := &slow{delays: []time.Duration{80 * time.Millisecond, time.Hour}}
	got, err := Hedged{Inner: inner, After: 20 * time.Millisecond}.Translate(context.Background(), "hi", "en", "es", Glossary{})
	if err != nil || got != "es:hi:a" {
		t.Fatalf("got %q, %v", got, err)
	}
	if n := inner.calls.Load(); n != 2 {
		t.Fatalf("%d calls, want 2", n)
	}
}

func TestHedgedEarlyErrorIsReturned(t *testing.T) {
	boom := errors.New("boom")
	inner := &slow{delays: []time.Duration{time.Millisecond}, err: boom}
	_, err := Hedged{Inner: inner, After: time.Hour}.Translate(context.Background(), "hi", "en", "es", Glossary{})
	if !errors.Is(err, boom) || inner.calls.Load() != 1 {
		t.Fatalf("err %v after %d calls", err, inner.calls.Load())
	}
}

func TestHedgedWaitsForTheOtherCallOnError(t *testing.T) {
	boom := errors.New("boom")
	inner := &errThenOK{err: boom}
	got, err := Hedged{Inner: inner, After: 10 * time.Millisecond}.Translate(context.Background(), "hi", "en", "es", Glossary{})
	if err != nil || got != "ok" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// errThenOK fails its first call slowly and answers the second.
type errThenOK struct {
	err   error
	calls atomic.Int32
}

func (e *errThenOK) Translate(context.Context, string, string, string, Glossary) (string, error) {
	if e.calls.Add(1) == 1 {
		time.Sleep(50 * time.Millisecond)
		return "", e.err
	}
	time.Sleep(100 * time.Millisecond)
	return "ok", nil
}

func TestHedgedRespectsContext(t *testing.T) {
	inner := &slow{delays: []time.Duration{time.Hour}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := (Hedged{Inner: inner, After: 10 * time.Millisecond}).Translate(ctx, "hi", "en", "es", Glossary{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestHedgedDefaultDelay(t *testing.T) {
	if DefaultHedgeAfter != 1500*time.Millisecond {
		t.Fatalf("DefaultHedgeAfter = %v", DefaultHedgeAfter)
	}
}
