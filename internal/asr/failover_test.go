// Copyright 2026 the Dubash contributors. Apache-2.0.

package asr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type engineFunc func(context.Context, Config, <-chan []byte, chan<- Event) error

func (f engineFunc) Run(c context.Context, g Config, a <-chan []byte, e chan<- Event) error {
	return f(c, g, a, e)
}

func TestFailoverReplaysOnlyUncommittedAudio(t *testing.T) {
	primary := engineFunc(func(ctx context.Context, _ Config, a <-chan []byte, e chan<- Event) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a:
		}
		send(ctx, e, Event{Kind: Final, Text: "committed", At: 1})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a:
		}
		return errors.New("injected primary outage")
	})
	count := make(chan int, 1)
	backup := engineFunc(func(ctx context.Context, _ Config, a <-chan []byte, e chan<- Event) error {
		n := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case b, ok := <-a:
				if !ok {
					count <- n
					send(ctx, e, Event{Kind: Final, Text: "backup", At: float64(n) / BytesPerSecond})
					return nil
				}
				n += len(b)
			}
		}
	})
	audio := make(chan []byte, 2)
	audio <- make([]byte, BytesPerSecond)
	audio <- make([]byte, BytesPerSecond)
	close(audio)
	events := make(chan Event, 32)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (&Failover{Primary: primary, Backup: backup}).Run(ctx, Config{}, audio, events); err != nil {
		t.Fatal(err)
	}
	if n := <-count; n != BytesPerSecond {
		t.Fatalf("replayed %d bytes, want one uncommitted second", n)
	}
	var finals []Event
	for len(events) > 0 {
		e := <-events
		if e.Kind == Final {
			finals = append(finals, e)
		}
	}
	if len(finals) != 2 || finals[0].At != 1 || finals[1].At != 2 {
		t.Fatalf("clock/dedup: %+v", finals)
	}
}

func TestFailoverOnRecoverableErrors(t *testing.T) {
	primary := engineFunc(func(ctx context.Context, _ Config, _ <-chan []byte, e chan<- Event) error {
		for range 3 {
			send(ctx, e, Event{Kind: Error, Text: "injected reconnect failure"})
		}
		<-ctx.Done()
		return ctx.Err()
	})
	backup := engineFunc(func(ctx context.Context, _ Config, a <-chan []byte, _ chan<- Event) error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case _, ok := <-a:
				if !ok {
					return nil
				}
			}
		}
	})
	audio := make(chan []byte, 1)
	audio <- []byte{1, 2}
	close(audio)
	events := make(chan Event, 32)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (&Failover{Primary: primary, Backup: backup}).Run(ctx, Config{}, audio, events); err != nil {
		t.Fatal(err)
	}
	switched := false
	for len(events) > 0 {
		if strings.Contains((<-events).Text, "switching to backup") {
			switched = true
		}
	}
	if !switched {
		t.Fatal("recovering primary never switched")
	}
}

func TestFailoverHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := engineFunc(func(c context.Context, _ Config, _ <-chan []byte, _ chan<- Event) error { <-c.Done(); return c.Err() })
	if err := (&Failover{Primary: blocked, Backup: blocked}).Run(ctx, Config{}, make(chan []byte), make(chan Event)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestFailoverProgressWatchdog(t *testing.T) {
	primary := engineFunc(func(ctx context.Context, _ Config, a <-chan []byte, _ chan<- Event) error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case _, ok := <-a:
				if !ok {
					<-ctx.Done()
					return ctx.Err()
				}
			}
		}
	})
	backup := engineFunc(func(ctx context.Context, _ Config, a <-chan []byte, e chan<- Event) error {
		n := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case b, ok := <-a:
				if !ok {
					send(ctx, e, Event{Kind: Final, Text: "recovered", At: float64(n) / BytesPerSecond})
					return nil
				}
				n += len(b)
			}
		}
	})
	audio := make(chan []byte, 1)
	audio <- make([]byte, BytesPerSecond)
	close(audio)
	events := make(chan Event, 32)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (&Failover{Primary: primary, Backup: backup, Stall: 10 * time.Millisecond}).Run(ctx, Config{}, audio, events); err != nil {
		t.Fatal(err)
	}
	found := false
	for len(events) > 0 {
		if e := <-events; e.Kind == Final && e.Text == "recovered" {
			found = true
		}
	}
	if !found {
		t.Fatal("stalled primary did not recover")
	}
}

func TestFailoverReportsBackupFailure(t *testing.T) {
	bad := engineFunc(func(context.Context, Config, <-chan []byte, chan<- Event) error { return errors.New("injected outage") })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := (&Failover{Primary: bad, Backup: bad}).Run(ctx, Config{}, make(chan []byte), make(chan Event, 8))
	if err == nil || !strings.Contains(err.Error(), "backup recognition failed") {
		t.Fatal(err)
	}
}

func TestFailoverBoundsReplayAndReportsLostAudio(t *testing.T) {
	primary := engineFunc(func(ctx context.Context, _ Config, _ <-chan []byte, _ chan<- Event) error {
		<-ctx.Done()
		return ctx.Err()
	})
	replayed := make(chan int, 1)
	backup := engineFunc(func(ctx context.Context, _ Config, a <-chan []byte, e chan<- Event) error {
		n := 0
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case b, ok := <-a:
				if !ok {
					replayed <- n
					send(ctx, e, Event{Kind: Final, Text: "tail", At: float64(n) / BytesPerSecond})
					return nil
				}
				n += len(b)
			}
		}
	})
	audio := make(chan []byte, 1)
	audio <- make([]byte, 3*BytesPerSecond)
	close(audio)
	events := make(chan Event, 8)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (&Failover{Primary: primary, Backup: backup, ReplaySeconds: 1}).Run(ctx, Config{}, audio, events); err != nil {
		t.Fatal(err)
	}
	if n := <-replayed; n != BytesPerSecond {
		t.Fatalf("unbounded replay: %d bytes", n)
	}
	warned, clock := false, false
	for len(events) > 0 {
		e := <-events
		if strings.Contains(e.Text, "older uncommitted audio was dropped") {
			warned = true
		}
		if e.Kind == Final && e.At == 3 {
			clock = true
		}
	}
	if !warned || !clock {
		t.Fatalf("audio loss warning=%v, original clock=%v", warned, clock)
	}
}
