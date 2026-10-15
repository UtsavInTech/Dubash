// Copyright 2026 the Dubash contributors. Apache-2.0.

package bench

import (
	"context"
	"strings"
	"time"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/lang"
)

// Recognition is what a recognizer made of one recording.
type Recognition struct {
	Text string `json:"text"`
	// FirstInterim and FirstFinal are measured from the first audio sent;
	// zero when the recognizer produced none.
	FirstInterim time.Duration `json:"firstInterimNs"`
	FirstFinal   time.Duration `json:"firstFinalNs"`
	// Lags are, per final (or per request for file APIs), how long after
	// its audio was sent the text arrived. See EngineRecognizer.
	Lags []time.Duration `json:"lagsNs"`
	// Tail is the time from the last audio sent to the last text.
	Tail time.Duration `json:"tailNs"`
	// BilledSeconds is the audio the provider charges for.
	BilledSeconds float64  `json:"billedSeconds"`
	Errors        []string `json:"errors,omitempty"`
}

// Recognizer transcribes one recording. vocab is the list of phrase
// hints, nil for the baseline run.
type Recognizer interface {
	Name() string
	Recognize(ctx context.Context, pcm []byte, lang string, vocab []string) (Recognition, error)
}

// chunkBytes is 100 ms of audio, the size the operator console and
// dubash-ingest send.
const chunkBytes = asr.BytesPerSecond / 10

// EngineRecognizer streams a recording into one of the server's own
// streaming engines (asr.Engine) the way the operator console does, so a
// streaming provider is measured through exactly the code that runs live.
//
// Speed paces the audio: 1 is real time (honest latency), 2 twice as fast,
// and 0 as fast as the engine reads it (quick, but latency is then
// meaningless). A final's lag is its arrival time minus the time the audio
// position it reports (asr.Event.At) was sent: for engines that report the
// end of the utterance it is the delay a reader sees after the speaker
// stops; for Gemini Live, which reports the audio consumed when the text
// arrived, it is a lower bound.
type EngineRecognizer struct {
	Label  string
	Engine asr.Engine
	Speed  float64
	// Drain is how long to keep listening after the last chunk for the
	// engine's last text (default 5 s); it ends sooner if the engine does.
	Drain time.Duration
}

// Name implements Recognizer.
func (e EngineRecognizer) Name() string { return e.Label }

// Recognize implements Recognizer.
func (e EngineRecognizer) Recognize(ctx context.Context, pcm []byte, lang string, vocab []string) (Recognition, error) {
	audio := make(chan []byte)
	events := make(chan asr.Event, 256)
	runErr := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		runErr <- e.Engine.Run(ctx, asr.Config{Lang: lang, Vocabulary: vocab}, audio, events)
		close(events)
	}()

	start := time.Now()
	sentAt := func(pos float64) time.Time { // when audio position pos (s) was sent
		if e.Speed <= 0 {
			return start
		}
		return start.Add(time.Duration(pos / e.Speed * float64(time.Second)))
	}
	sendDone := make(chan time.Time, 1)
	go func() {
		defer close(audio)
		for off := 0; off < len(pcm); off += chunkBytes {
			if e.Speed > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Until(sentAt(float64(off) / asr.BytesPerSecond))):
				}
			}
			select {
			case audio <- pcm[off:min(off+chunkBytes, len(pcm))]:
			case <-ctx.Done():
				return
			}
		}
		sendDone <- time.Now()
	}()

	drain := e.Drain
	if drain <= 0 {
		drain = 5 * time.Second
	}
	var (
		rec      Recognition
		finals   []string
		lastText time.Time
		lastSent time.Time
		deadline <-chan time.Time
	)
	for {
		select {
		case t := <-sendDone:
			lastSent = t
			deadline = time.After(drain)
		case <-deadline:
			cancel()
			deadline = nil
		case ev, ok := <-events:
			if !ok {
				rec.Text = strings.Join(finals, " ")
				if !lastSent.IsZero() && lastText.After(lastSent) {
					rec.Tail = lastText.Sub(lastSent)
				}
				rec.BilledSeconds = Seconds(pcm)
				if err := <-runErr; err != nil && ctx.Err() == nil {
					return rec, err
				}
				return rec, nil
			}
			now := time.Now()
			switch ev.Kind {
			case asr.Interim:
				if rec.FirstInterim == 0 {
					rec.FirstInterim = now.Sub(start)
				}
			case asr.Final:
				if rec.FirstFinal == 0 {
					rec.FirstFinal = now.Sub(start)
				}
				finals = append(finals, ev.Text)
				rec.Lags = append(rec.Lags, max(0, now.Sub(sentAt(ev.At))))
				lastText = now
			case asr.Error:
				rec.Errors = append(rec.Errors, ev.Text)
			}
		}
	}
}

// ScriptedEngine is a fake recognizer that "hears" the reference
// sentence by sentence, optionally damaged by Mishear (a whole-word
// replacement map), so the harness and the report can be checked
// without any API call.
func ScriptedEngine(lang, ref string, seconds float64, mishear map[string]string) asr.Engine {
	lines := Sentences(ref)
	for i, l := range lines {
		words := strings.Fields(l)
		for j, w := range words {
			if r, ok := mishear[w]; ok {
				words[j] = r
			}
		}
		lines[i] = strings.Join(words, " ")
	}
	every := seconds / float64(max(len(lines), 1))
	return asr.Fake{Lines: map[string][]string{lang: lines}, Every: max(every, 0.2)}
}

// BCP47 returns the Sarvam tag for code (en-IN, hi-IN…), or code itself
// if Sarvam does not know it.
func BCP47(code string) string {
	if t := lang.Sarvam(code); t != "" {
		return t
	}
	return code
}

// Sentences splits text after ., ?, !, the danda (।) and the double
// danda (॥) when a space or the end follows. It is the benchmark's unit
// of translation, as a committed caption line is live.
func Sentences(text string) []string {
	var out []string
	var b strings.Builder
	rs := []rune(strings.TrimSpace(text))
	for i, r := range rs {
		b.WriteRune(r)
		if strings.ContainsRune(".?!।॥", r) && (i+1 == len(rs) || rs[i+1] == ' ' || rs[i+1] == '\n') {
			if s := strings.TrimSpace(b.String()); s != "" {
				out = append(out, s)
			}
			b.Reset()
		}
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// fmtErr keeps provider error messages short in the report.
func fmtErr(err error) string {
	s := []rune(err.Error())
	if len(s) > 200 {
		return string(s[:200]) + "…"
	}
	return string(s)
}
