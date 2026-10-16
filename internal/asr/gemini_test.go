package asr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/genai"
)

// fakeLive is a scripted Live session. onEnd runs when the client ends
// the audio stream, typically to make the "server" finish a transcript.
type fakeLive struct {
	msgs      chan *genai.LiveServerMessage
	closed    chan struct{}
	closeOnce sync.Once
	closes    atomic.Int32
	audio     atomic.Int64
	ends      atomic.Int32
	onEnd     func(*fakeLive)
	stalled   atomic.Bool // sends block until Close, like a dead TCP peer
}

func newFakeLive() *fakeLive {
	return &fakeLive{msgs: make(chan *genai.LiveServerMessage, 16), closed: make(chan struct{})}
}

func (f *fakeLive) SendRealtimeInput(in genai.LiveRealtimeInput) error {
	select {
	case <-f.closed:
		return errors.New("use of closed session")
	default:
	}
	if f.stalled.Load() {
		<-f.closed
		return errors.New("use of closed session")
	}
	if in.Audio != nil {
		f.audio.Add(int64(len(in.Audio.Data)))
	}
	if in.AudioStreamEnd {
		f.ends.Add(1)
		if f.onEnd != nil {
			f.onEnd(f)
		}
	}
	return nil
}

func (f *fakeLive) Receive() (*genai.LiveServerMessage, error) {
	select {
	case m := <-f.msgs:
		return m, nil
	case <-f.closed:
		return nil, errors.New("connection closed")
	}
}

func (f *fakeLive) Close() error {
	f.closes.Add(1)
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func finished(text string) *genai.LiveServerMessage {
	return &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: text, Finished: true},
	}}
}

func interim(text string) *genai.LiveServerMessage {
	return &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InterimInputTranscription: &genai.Transcription{Text: text},
	}}
}

// generationComplete answers the end of the audio stream with nothing to
// add, as the real server closes an utterance.
func generationComplete(f *fakeLive) {
	f.msgs <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{GenerationComplete: true}}
}

// harness runs a Gemini engine in the background and records its events.
type harness struct {
	audio  chan []byte
	events chan Event
	done   chan error
	cancel context.CancelFunc
}

func start(t *testing.T, g *Gemini) *harness {
	t.Helper()
	if g.Log == nil {
		g.Log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{audio: make(chan []byte), events: make(chan Event, 256), done: make(chan error, 1), cancel: cancel}
	go func() { h.done <- g.Run(ctx, Config{Lang: "en"}, h.audio, h.events) }()
	t.Cleanup(cancel)
	return h
}

// send delivers n 100 ms chunks, failing if the engine stops reading.
func (h *harness) send(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case h.audio <- make([]byte, BytesPerSecond/10):
		case <-time.After(time.Second):
			t.Fatal("engine stopped reading audio")
		}
	}
}

func (h *harness) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-h.done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func (h *harness) next(t *testing.T, kind Kind) Event {
	t.Helper()
	for {
		select {
		case ev := <-h.events:
			if ev.Kind == kind {
				return ev
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no event of kind %d", kind)
		}
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestGeminiReadsAudioWhileConnecting(t *testing.T) {
	release := make(chan struct{})
	sess := newFakeLive()
	g := &Gemini{
		drainWait: 100 * time.Millisecond, // how long held audio may wait for a session
		dial: func(context.Context, Config) (liveSession, error) {
			<-release // a dial that hangs, like a stalled handshake
			return sess, nil
		},
	}
	h := start(t, g)
	h.send(t, 80) // far more than any channel buffer
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	close(release)
	eventually(t, "a session opened after Run returned was not closed", func() bool { return sess.closes.Load() == 1 })
}

func TestGeminiReturnsWhenAudioEndsDuringBackoff(t *testing.T) {
	g := &Gemini{
		firstBackoff: time.Hour,
		dial:         func(context.Context, Config) (liveSession, error) { return nil, errors.New("network down") },
	}
	h := start(t, g)
	if ev := h.next(t, Error); !strings.Contains(ev.Text, "network down") {
		t.Fatalf("error event = %q", ev.Text)
	}
	h.send(t, 20)
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
}

func TestGeminiFailsOnRejectedSetup(t *testing.T) {
	var dials atomic.Int32
	g := &Gemini{
		firstBackoff: time.Millisecond,
		dial: func(context.Context, Config) (liveSession, error) {
			dials.Add(1)
			return nil, fmt.Errorf("failed to receive setup complete: %w",
				&websocket.CloseError{Code: websocket.CloseInvalidFramePayloadData, Text: "API key not valid"})
		},
	}
	h := start(t, g)
	err := h.wait(t)
	if err == nil || !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("err = %v, want the setup rejection", err)
	}
	if n := dials.Load(); n != 1 {
		t.Fatalf("dialed %d times, want 1 (not retryable)", n)
	}
}

func TestGeminiGivesUpAfterRepeatedConnectFailures(t *testing.T) {
	var dials atomic.Int32
	g := &Gemini{
		firstBackoff: time.Millisecond,
		dial: func(context.Context, Config) (liveSession, error) {
			dials.Add(1)
			return nil, errors.New("connection refused")
		},
	}
	h := start(t, g)
	err := h.wait(t)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v", err)
	}
	if n := dials.Load(); n != maxConnectFailures {
		t.Fatalf("dialed %d times, want %d", n, maxConnectFailures)
	}
}

func TestGeminiRotationDrainsBeforeClosing(t *testing.T) {
	first, second := newFakeLive(), newFakeLive()
	first.onEnd = func(f *fakeLive) { f.msgs <- finished("Last words before rotation.") }
	second.onEnd = generationComplete
	sessions := []*fakeLive{first, second}
	var dials atomic.Int32
	g := &Gemini{
		MaxSession: 100 * time.Millisecond,
		drainWait:  time.Hour, // a finished transcript, not the timeout, ends the drain
		dial: func(context.Context, Config) (liveSession, error) {
			i := dials.Add(1) - 1
			if int(i) >= len(sessions) {
				return nil, errors.New("no more sessions")
			}
			return sessions[i], nil
		},
	}
	h := start(t, g)
	h.send(t, 3)
	if ev := h.next(t, Final); ev.Text != "Last words before rotation." {
		t.Fatalf("final = %q", ev.Text)
	}
	eventually(t, "first session not closed after draining", func() bool { return first.closes.Load() == 1 })
	if first.ends.Load() != 1 {
		t.Fatal("rotation must end the audio stream before closing")
	}
	h.send(t, 5)
	eventually(t, "audio did not reach the next session", func() bool { return second.audio.Load() > 0 })
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	if second.closes.Load() != 1 {
		t.Fatalf("second session closed %d times", second.closes.Load())
	}
	// Every consumed byte is on the clock, across the rotation.
	if got, want := first.audio.Load()+second.audio.Load(), int64(8*BytesPerSecond/10); got != want {
		t.Fatalf("sent %d bytes, want %d", got, want)
	}
}

func TestGeminiGoAwayDrainsAndReconnects(t *testing.T) {
	first, second := newFakeLive(), newFakeLive()
	first.onEnd = func(f *fakeLive) { f.msgs <- finished("Said before go away.") }
	second.onEnd = generationComplete
	sessions := []*fakeLive{first, second}
	var dials atomic.Int32
	g := &Gemini{
		drainWait: time.Hour,
		dial: func(context.Context, Config) (liveSession, error) {
			i := dials.Add(1) - 1
			if int(i) >= len(sessions) {
				return nil, errors.New("no more sessions")
			}
			return sessions[i], nil
		},
	}
	h := start(t, g)
	h.send(t, 3)
	first.msgs <- &genai.LiveServerMessage{GoAway: &genai.LiveServerGoAway{TimeLeft: 30 * time.Second}}
	if ev := h.next(t, Final); ev.Text != "Said before go away." {
		t.Fatalf("final = %q", ev.Text)
	}
	eventually(t, "no reconnect after go away", func() bool { return dials.Load() == 2 })
	h.send(t, 2)
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	if first.closes.Load() != 1 || second.closes.Load() != 1 {
		t.Fatalf("closes = %d, %d", first.closes.Load(), second.closes.Load())
	}
	select {
	case ev := <-h.events:
		if ev.Kind == Error {
			t.Fatalf("go away is not an error: %q", ev.Text)
		}
	default:
	}
}

func TestGeminiStopFlushesTrailingText(t *testing.T) {
	sess := newFakeLive()
	sess.onEnd = func(f *fakeLive) {
		f.msgs <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
			InputTranscription: &genai.Transcription{Text: "and one more thing"},
		}}
	}
	g := &Gemini{
		drainWait: time.Hour, // the unfinished utterance final ends the drain
		dial:      func(context.Context, Config) (liveSession, error) { return sess, nil },
	}
	h := start(t, g)
	h.send(t, 2)
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	if ev := h.next(t, Final); ev.Text != "and one more thing" {
		t.Fatalf("final = %q", ev.Text)
	}
	if sess.closes.Load() != 1 {
		t.Fatalf("closed %d times", sess.closes.Load())
	}
}

func TestGeminiCancelClosesSession(t *testing.T) {
	sess := newFakeLive()
	g := &Gemini{dial: func(context.Context, Config) (liveSession, error) { return sess, nil }}
	h := start(t, g)
	h.send(t, 2)
	h.cancel()
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	eventually(t, "session not closed", func() bool { return sess.closes.Load() == 1 })
}

func TestGeminiReconnectsAfterSessionError(t *testing.T) {
	first, second := newFakeLive(), newFakeLive()
	second.onEnd = generationComplete
	sessions := []*fakeLive{first, second}
	var dials atomic.Int32
	g := &Gemini{
		firstBackoff: time.Millisecond,
		dial: func(context.Context, Config) (liveSession, error) {
			i := dials.Add(1) - 1
			if int(i) >= len(sessions) {
				return nil, errors.New("no more sessions")
			}
			return sessions[i], nil
		},
	}
	h := start(t, g)
	h.send(t, 1)
	first.Close() // the server drops the connection
	if ev := h.next(t, Error); !strings.Contains(ev.Text, "closed") {
		t.Fatalf("error event = %q", ev.Text)
	}
	h.send(t, 3)
	eventually(t, "audio did not reach the new session", func() bool { return second.audio.Load() > 0 })
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
}

func TestGeminiAudioEndingDuringRotationStillReachesNextSession(t *testing.T) {
	first, second := newFakeLive(), newFakeLive()
	second.onEnd = generationComplete
	sessions := []*fakeLive{first, second}
	var dials atomic.Int32
	g := &Gemini{
		MaxSession: 50 * time.Millisecond,
		drainWait:  300 * time.Millisecond, // the first session never finishes: time out
		dial: func(context.Context, Config) (liveSession, error) {
			i := dials.Add(1) - 1
			if int(i) >= len(sessions) {
				return nil, errors.New("no more sessions")
			}
			return sessions[i], nil
		},
	}
	h := start(t, g)
	h.send(t, 1)
	eventually(t, "rotation did not start", func() bool { return first.ends.Load() == 1 })
	h.send(t, 4) // held while the first session drains
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	if got, want := second.audio.Load(), int64(4*BytesPerSecond/10); got != want {
		t.Fatalf("next session got %d bytes, want %d", got, want)
	}
	if first.closes.Load() != 1 || second.closes.Load() != 1 || second.ends.Load() != 1 {
		t.Fatalf("closes %d/%d, second ends %d", first.closes.Load(), second.closes.Load(), second.ends.Load())
	}
}

func TestGeminiCommitsSentencesFromInterims(t *testing.T) {
	sess := newFakeLive()
	sess.onEnd = generationComplete
	g := &Gemini{dial: func(context.Context, Config) (liveSession, error) { return sess, nil }}
	h := start(t, g)
	h.send(t, 2)
	sess.msgs <- interim("Welcome to Nerdearla. Today, we")
	if ev := h.next(t, Final); ev.Text != "Welcome to Nerdearla." {
		t.Fatalf("final = %+v", ev)
	}
	if ev := h.next(t, Interim); ev.Text != "Today, we" {
		t.Fatalf("interim = %q", ev.Text)
	}
	sess.msgs <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: "Welcome to Nerdearla. Today, we talk."},
	}}
	if ev := h.next(t, Final); ev.Text != "Today, we talk." {
		t.Fatalf("utterance final = %q", ev.Text)
	}
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
}

func TestGeminiCancelUnblocksStalledSend(t *testing.T) {
	sess := newFakeLive()
	g := &Gemini{dial: func(context.Context, Config) (liveSession, error) { return sess, nil }}
	h := start(t, g)
	h.send(t, 1)
	eventually(t, "audio did not reach the session", func() bool { return sess.audio.Load() > 0 })
	sess.stalled.Store(true)
	h.send(t, 1) // the engine is now stuck writing to the dead connection
	h.cancel()
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	if n := sess.closes.Load(); n != 1 {
		t.Fatalf("closed %d times, want 1", n)
	}
}

func TestGeminiDrainEndsOnRealUtteranceSignals(t *testing.T) {
	cases := map[string]func(*fakeLive){
		"input transcription": func(f *fakeLive) {
			f.msgs <- &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
				InputTranscription: &genai.Transcription{Text: "Closing words."}, // never Finished
			}}
		},
		"generation complete": generationComplete,
	}
	for name, onEnd := range cases {
		t.Run(name, func(t *testing.T) {
			sess := newFakeLive()
			sess.onEnd = onEnd
			g := &Gemini{
				drainWait: time.Hour, // only the server's signal can end the drain in time
				dial:      func(context.Context, Config) (liveSession, error) { return sess, nil },
			}
			h := start(t, g)
			h.send(t, 2)
			close(h.audio)
			if err := h.wait(t); err != nil {
				t.Fatal(err)
			}
			if sess.closes.Load() != 1 {
				t.Fatalf("closed %d times", sess.closes.Load())
			}
		})
	}
}

// voiced is a 100 ms chunk loud enough to count as speech.
func voiced() []byte {
	b := make([]byte, BytesPerSecond/10)
	for i := 0; i < len(b); i += 2 {
		v := int16(3000)
		if i%4 == 0 {
			v = -3000
		}
		b[i], b[i+1] = byte(v), byte(uint16(v)>>8)
	}
	return b
}

func (h *harness) sendVoiced(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case h.audio <- voiced():
		case <-time.After(time.Second):
			t.Fatal("engine stopped reading audio")
		}
	}
}

func TestGeminiReplacesStalledSessionAndReplaysItsAudio(t *testing.T) {
	first, second := newFakeLive(), newFakeLive()
	first.onEnd = func(f *fakeLive) { f.msgs <- finished("Welcome to Nerdearla.") }
	second.onEnd = generationComplete
	sessions := []*fakeLive{first, second}
	var dials atomic.Int32
	g := &Gemini{
		stallWait: 500 * time.Millisecond,
		drainWait: time.Hour,
		dial: func(context.Context, Config) (liveSession, error) {
			i := dials.Add(1) - 1
			if int(i) >= len(sessions) {
				return nil, errors.New("no more sessions")
			}
			return sessions[i], nil
		},
	}
	h := start(t, g)
	h.sendVoiced(t, 1)
	first.msgs <- interim("Welcome to Nerdearla.")
	h.next(t, Interim)
	// The model keeps repeating itself while speech flows in.
	for range 5 {
		first.msgs <- interim("Welcome to Nerdearla.")
		h.sendVoiced(t, 1)
	}
	if ev := h.next(t, Error); !strings.Contains(ev.Text, "stalled") {
		t.Fatalf("error = %q", ev.Text)
	}
	if ev := h.next(t, Final); ev.Text != "Welcome to Nerdearla." {
		t.Fatalf("final = %q", ev.Text)
	}
	eventually(t, "stuck session not replaced", func() bool { return dials.Load() == 2 })
	if first.ends.Load() != 1 || first.closes.Load() != 1 {
		t.Fatalf("stuck session ends %d closes %d", first.ends.Load(), first.closes.Load())
	}
	h.sendVoiced(t, 1)
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	// The next session hears the speech the stuck one never transcribed.
	if got, min := second.audio.Load(), int64(5*BytesPerSecond/10); got < min {
		t.Fatalf("next session got %d bytes, want at least %d", got, min)
	}
}

func TestGeminiKeepsSessionWhileTranscriptMovesOrRoomIsSilent(t *testing.T) {
	sess := newFakeLive()
	sess.onEnd = generationComplete
	var dials atomic.Int32
	g := &Gemini{
		stallWait: 300 * time.Millisecond,
		dial: func(context.Context, Config) (liveSession, error) {
			dials.Add(1)
			return sess, nil
		},
	}
	h := start(t, g)
	h.send(t, 10) // silence never looks like a stall
	words := strings.Fields("one two three four five six seven eight")
	for i := range words {
		sess.msgs <- interim(strings.Join(words[:i+1], " "))
		h.next(t, Interim)
		h.sendVoiced(t, 1)
	}
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	if n := dials.Load(); n != 1 {
		t.Fatalf("dialed %d sessions, want 1", n)
	}
}

func TestGeminiCommitsASentenceWhenTheSpeakerPauses(t *testing.T) {
	sess := newFakeLive()
	sess.onEnd = generationComplete
	g := &Gemini{
		settleWait: 200 * time.Millisecond,
		dial:       func(context.Context, Config) (liveSession, error) { return sess, nil },
	}
	h := start(t, g)
	h.send(t, 2)
	sess.msgs <- interim("Thank you all for coming.")
	h.next(t, Interim)
	// No more words and no utterance final: the pause alone commits it.
	start := time.Now()
	if ev := h.next(t, Final); ev.Text != "Thank you all for coming." {
		t.Fatalf("final = %q", ev.Text)
	}
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("committed after %v, before the interim settled", d)
	}
	sess.msgs <- finished("Thank you all for coming.")
	close(h.audio)
	if err := h.wait(t); err != nil {
		t.Fatal(err)
	}
	for len(h.events) > 0 {
		if ev := <-h.events; ev.Kind == Final {
			t.Fatalf("sentence emitted twice: %q", ev.Text)
		}
	}
}
