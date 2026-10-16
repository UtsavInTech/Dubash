// Copyright 2026 the Dubash contributors. Apache-2.0.

package asr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/UtsavInTech/dubash/internal/resilience"
)

// Failover switches once per transmission to an independent backup after a
// fatal return, consecutive provider errors, or a stalled stream. Uncommitted
// audio is replayed up to ReplaySeconds (default 15); older audio is explicitly
// reported as lost. Final timestamps stay on the original transmission clock.
// Providers must honor cancellation, as required by Engine.
type Failover struct {
	Primary, Backup Engine
	ErrorThreshold  int
	Stall           time.Duration
	ReplaySeconds   int
}

func (f *Failover) Run(ctx context.Context, cfg Config, audio <-chan []byte, events chan<- Event) error {
	threshold := f.ErrorThreshold
	if threshold <= 0 {
		threshold = 3
	}
	stall := f.Stall
	if stall <= 0 {
		stall = 20 * time.Second
	}
	seconds := f.ReplaySeconds
	if seconds <= 0 {
		seconds = 15
	}
	limit := seconds * BytesPerSecond
	ctx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()
	var input chan []byte
	var updates chan Event
	var done chan error
	var stop context.CancelFunc
	var pending [][]byte
	var history []byte
	var received int64
	var base, committedAt float64
	var ended, backup bool
	failures := 0
	lastProgress, lastAudio := time.Now(), time.Time{}
	start := func(engine Engine) {
		runCtx, cancel := context.WithCancel(ctx)
		stop = cancel
		input, updates, done = make(chan []byte), make(chan Event, 128), make(chan error, 1)
		in, out, result := input, updates, done
		go func() { result <- engine.Run(runCtx, cfg, in, out) }()
		lastProgress = time.Now()
	}
	forward := func(e Event) {
		e.At += base
		if e.Kind == Final {
			if e.At < committedAt {
				return
			}
			committedAt = e.At
		}
		if e.Kind != Error {
			lastProgress, failures = time.Now(), 0
		}
		send(ctx, events, e)
	}
	switchToBackup := func(cause error) error {
		if backup {
			return fmt.Errorf("backup recognition failed: %w", cause)
		}
		stop()
		// Discard any outstanding primary events: only the commits already delivered
		// to callers define the replay boundary.
		skip := int64(committedAt * BytesPerSecond)
		historyStart := received - int64(len(history))
		if skip < historyStart {
			send(ctx, events, Event{Kind: Error, At: committedAt, Text: "recognition outage exceeded replay buffer; older uncommitted audio was dropped"})
			skip = historyStart
		}
		skip = min(max(skip, historyStart), received)
		replay := append([]byte(nil), history[skip-historyStart:]...)
		base = float64(skip) / BytesPerSecond
		pending = nil
		// Keep chunks small enough for streaming providers and bounded queues.
		for len(replay) > 0 {
			n := min(len(replay), BytesPerSecond/10)
			pending = append(pending, replay[:n:n])
			replay = replay[n:]
		}
		backup, failures = true, 0
		resilience.ASRSwitches.Add(1)
		send(ctx, events, Event{Kind: Error, At: base, Text: "switching to backup recognition: " + cause.Error()})
		start(f.Backup)
		return nil
	}
	start(f.Primary)
	defer func() { stop() }()
	tick := time.NewTicker(min(stall/4, time.Second))
	defer tick.Stop()
	for {
		var output chan []byte
		var chunk []byte
		if len(pending) > 0 {
			output, chunk = input, pending[0]
		}
		if ended && len(pending) == 0 && input != nil {
			close(input)
			input = nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case data, ok := <-audio:
			if !ok {
				ended, audio = true, nil
				continue
			}
			if len(data) == 0 {
				continue
			}
			// Own the bytes: microphone producers may reuse their buffers.
			data = append([]byte(nil), data...)
			received += int64(len(data))
			if lastAudio.IsZero() || time.Since(lastAudio) >= stall {
				lastProgress = time.Now()
			}
			lastAudio = time.Now()
			history = append(history, data...)
			if len(history) > limit {
				history = append([]byte(nil), history[len(history)-limit:]...)
			}
			pending = append(pending, data)
			queued := 0
			for _, p := range pending {
				queued += len(p)
			}
			if queued > limit {
				if err := switchToBackup(errors.New("recognizer stopped consuming audio")); err != nil {
					return err
				}
			}
		case output <- chunk:
			pending = pending[1:]
		case e := <-updates:
			forward(e)
			if e.Kind == Error {
				failures++
				if failures >= threshold && !backup {
					if err := switchToBackup(errors.New(e.Text)); err != nil {
						return err
					}
				}
			}
		case err := <-done:
			// Providers may enqueue their last final immediately before returning.
			for len(updates) > 0 {
				forward(<-updates)
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == nil && ended && len(pending) == 0 {
				return nil
			}
			if err == nil {
				err = errors.New("recognizer exited before audio ended")
			}
			if err = switchToBackup(err); err != nil {
				return err
			}
		case <-tick.C:
			// Idle/paused microphones must not trigger failover.
			if !lastAudio.IsZero() && (time.Since(lastAudio) < stall || ended) && time.Since(lastProgress) >= stall {
				if err := switchToBackup(errors.New("recognition progress deadline exceeded")); err != nil {
					return err
				}
			}
		}
	}
}
