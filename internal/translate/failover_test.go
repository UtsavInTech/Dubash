// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type translatorFunc func(context.Context, string, string, string, Glossary) (string, error)

func (f translatorFunc) Translate(c context.Context, t, s, d string, g Glossary) (string, error) {
	return f(c, t, s, d, g)
}

func TestFailoverDeadlineCircuitAndRecovery(t *testing.T) {
	var calls atomic.Int64
	primary := translatorFunc(func(ctx context.Context, _, _, _ string, _ Glossary) (string, error) {
		if calls.Add(1) == 1 {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return "recovered", nil
	})
	f := &Failover{Primary: primary, Backup: Fake{}, Timeout: 5 * time.Millisecond, Cooldown: 20 * time.Millisecond, Threshold: 1}
	out, err := f.Translate(context.Background(), "hello", "hi", "en", Glossary{})
	if err != nil || out != "[en] hello" {
		t.Fatalf("fallback: %q %v", out, err)
	}
	if _, err = f.Translate(context.Background(), "hello", "hi", "en", Glossary{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("open circuit called primary")
	}
	time.Sleep(30 * time.Millisecond)
	out, err = f.Translate(context.Background(), "hello", "hi", "en", Glossary{})
	if err != nil || out != "recovered" {
		t.Fatalf("probe: %q %v", out, err)
	}
}

func TestFailoverCancellationAndBothProvidersDown(t *testing.T) {
	var backups atomic.Int64
	bad := translatorFunc(func(context.Context, string, string, string, Glossary) (string, error) {
		return "", errors.New("injected outage")
	})
	backup := translatorFunc(func(context.Context, string, string, string, Glossary) (string, error) {
		backups.Add(1)
		return "", errors.New("backup outage")
	})
	f := &Failover{Primary: bad, Backup: backup}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Translate(ctx, "hello", "hi", "en", Glossary{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if backups.Load() != 0 {
		t.Fatal("cancelled request billed backup")
	}
	if _, err := f.Translate(context.Background(), "hello", "hi", "en", Glossary{}); err == nil {
		t.Fatal("both-provider outage hidden")
	}
}
