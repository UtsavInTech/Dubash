// Package asr turns live PCM audio into transcript events.
package asr

import (
	"context"
	"math"
)

// BytesPerSecond of the wire format: PCM16 mono at 16 kHz.
const BytesPerSecond = 16000 * 2

// Kind of transcript event.
type Kind int

const (
	// Interim is a provisional hypothesis for speech still in progress.
	Interim Kind = iota
	// Final is a closed segment that will not change.
	Final
	// Error reports a recoverable failure (the engine keeps running).
	Error
)

// Event is one transcript update. At is the audio position, in seconds of
// audio consumed by the engine during this Run, when the event was produced.
type Event struct {
	Kind Kind
	Text string
	At   float64
}

// Config describes a room's audio to the engine.
type Config struct {
	Lang       string   // ISO 639-1 code from internal/lang: "en", "hi", "te"…
	Vocabulary []string // terms to bias recognition towards
	// Mode is a provider's recognition style, empty for its default. For
	// Sarvam, "codemix" writes English words in Latin script inside
	// Indian-language text.
	Mode string
	// Prompt is a short context hint (the event's subject), for providers
	// that take one.
	Prompt string
}

// Engine transcribes a stream of PCM chunks. Run blocks until audio is
// closed or ctx is done, handles reconnects internally (reporting them as
// Error events) and returns only unrecoverable errors. It never closes
// events.
type Engine interface {
	Run(ctx context.Context, cfg Config, audio <-chan []byte, events chan<- Event) error
}

func send(ctx context.Context, events chan<- Event, e Event) {
	select {
	case events <- e:
	case <-ctx.Done():
	}
}

// RMS is the root mean square level of PCM16 little-endian samples, from
// 0 (digital silence) to 32768 (full scale).
func RMS(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := range n {
		v := float64(int16(uint16(pcm[2*i]) | uint16(pcm[2*i+1])<<8))
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}

// DBFS converts an RMS level to decibels relative to full scale, floored
// at -99 so silence stays a finite number.
func DBFS(rms float64) float64 {
	if rms <= 0 {
		return -99
	}
	return max(-99, 20*math.Log10(rms/32768))
}
