// Copyright 2026 the Dubash contributors. Apache-2.0.

package asr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeSarvam is a realtime server that follows the wire format observed on
// 2026-10-16. Each connection runs script with a helper.
type fakeSarvam struct {
	t      *testing.T
	script func(fs *fakeConn)
	conns  atomic.Int32
	mu     sync.Mutex
	query  []string // raw query of each connection
	header []string // subscription key of each connection
}

type fakeConn struct {
	t     *testing.T
	c     *websocket.Conn
	ctx   context.Context
	n     int // connection number, from 1
	audio int // bytes received
	pings int
	ended bool
}

func (f *fakeSarvam) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.query = append(f.query, r.URL.RawQuery)
	f.header = append(f.header, r.Header.Get("Api-Subscription-Key"))
	f.mu.Unlock()
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	fc := &fakeConn{t: f.t, c: c, ctx: r.Context(), n: int(f.conns.Add(1))}
	fc.send(map[string]any{"event": "session.begin"})
	f.script(fc)
}

func (fc *fakeConn) send(v any) {
	data, _ := json.Marshal(v)
	fc.c.Write(fc.ctx, websocket.MessageText, data)
}

func (fc *fakeConn) partial(text string) {
	fc.send(map[string]any{"event": "transcript.partial", "utterance_idx": 0, "text": text})
}

func (fc *fakeConn) final(text string) {
	fc.send(map[string]any{"event": "transcript.final", "utterance_idx": 0, "text": text})
}

// read reads one client message and accounts for it; false when closed.
func (fc *fakeConn) read() (string, bool) {
	_, data, err := fc.c.Read(fc.ctx)
	if err != nil {
		return "", false
	}
	var m struct{ Event, Audio string }
	json.Unmarshal(data, &m)
	switch m.Event {
	case "audio_input":
		fc.audio += len(m.Audio) * 3 / 4
	case "ping":
		fc.pings++
		fc.send(map[string]any{"event": "pong"})
	case "end":
		fc.ended = true
	}
	return m.Event, true
}

// until reads until the client has sent bytes of audio or "end".
func (fc *fakeConn) until(bytes int) bool {
	for fc.audio < bytes && !fc.ended {
		if _, ok := fc.read(); !ok {
			return false
		}
	}
	return true
}

func (fc *fakeConn) untilEnd() {
	for !fc.ended {
		if _, ok := fc.read(); !ok {
			return
		}
	}
}

func newSarvam(t *testing.T, script func(*fakeConn)) (*Sarvam, *fakeSarvam) {
	f := &fakeSarvam{t: t, script: script}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &Sarvam{
		Key: "k", URL: "ws" + strings.TrimPrefix(srv.URL, "http"),
		backoff: 10 * time.Millisecond, drain: 500 * time.Millisecond,
	}, f
}

// stream runs s over seconds of audio (sent as fast as read) and returns
// every event and Run's error.
func stream(t *testing.T, s *Sarvam, cfg Config, seconds float64, pace time.Duration) ([]Event, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	audio := make(chan []byte)
	events := make(chan Event, 256)
	errc := make(chan error, 1)
	go func() { errc <- s.Run(ctx, cfg, audio, events); close(events) }()
	go func() {
		defer close(audio)
		for range int(seconds * 10) {
			select {
			case audio <- make([]byte, BytesPerSecond/10):
			case <-ctx.Done():
				return
			}
			time.Sleep(pace)
		}
	}()
	var evs []Event
	for e := range events {
		evs = append(evs, e)
	}
	return evs, <-errc
}

func TestSarvamTranscribes(t *testing.T) {
	s, f := newSarvam(t, func(fc *fakeConn) {
		fc.until(BytesPerSecond / 2)
		fc.partial("नमस्ते")
		fc.partial("नमस्ते") // Sarvam repeats partials
		fc.until(BytesPerSecond)
		fc.partial("नमस्ते दोस्तों")
		fc.untilEnd()
		fc.final("नमस्ते दोस्तों। आज कथा है।")
		fc.send(map[string]any{"event": "session.end", "audio_duration_s": 2})
	})
	evs, err := stream(t, s, Config{Lang: "hi", Mode: "codemix", Vocabulary: []string{"बागेश्वर धाम", "Sarvam"}}, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(evs, Final); strings.Join(got, "|") != "नमस्ते दोस्तों।|आज कथा है।" {
		t.Errorf("finals %q", got)
	}
	if got := texts(evs, Interim); len(got) != 2 || got[0] != "नमस्ते" || got[1] != "नमस्ते दोस्तों" {
		t.Errorf("interims %q (duplicates must be dropped)", got)
	}
	q := f.query[0]
	for _, want := range []string{"language_code=hi-IN", "model=saaras%3Av4", "mode=codemix", "silence_duration_ms=500", "keyterms=%5B%22"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q lacks %q", q, want)
		}
	}
	if f.header[0] != "k" {
		t.Errorf("key header %q", f.header[0])
	}
}

func TestSarvamReconnectsAndKeepsAudio(t *testing.T) {
	var second atomic.Int64
	s, f := newSarvam(t, func(fc *fakeConn) {
		if fc.n == 1 {
			fc.until(BytesPerSecond / 2)
			fc.partial("पहला")
			fc.c.Close(websocket.StatusInternalError, "boom")
			return
		}
		fc.untilEnd()
		second.Store(int64(fc.audio))
		fc.final("दूसरा सत्र।")
		fc.send(map[string]any{"event": "session.end"})
	})
	evs, err := stream(t, s, Config{Lang: "hi"}, 2, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if f.conns.Load() != 2 {
		t.Fatalf("%d connections", f.conns.Load())
	}
	// The first session's uncommitted text is carried, not lost.
	if got := strings.Join(texts(evs, Final), "|"); got != "पहला दूसरा सत्र।" {
		t.Errorf("finals %q", got)
	}
	if len(texts(evs, Error)) == 0 {
		t.Error("lost session not reported")
	}
	if second.Load() == 0 {
		t.Error("no audio reached the second session")
	}
}

func TestSarvamFatalError(t *testing.T) {
	s, f := newSarvam(t, func(fc *fakeConn) {
		fc.send(map[string]any{"event": "error", "code": "unauthorized", "is_fatal": true, "message": "Invalid subscription key"})
		fc.untilEnd()
	})
	_, err := stream(t, s, Config{Lang: "hi"}, 1, 0)
	if err == nil || !strings.Contains(err.Error(), "Invalid subscription key") {
		t.Errorf("err %v", err)
	}
	if f.conns.Load() != 1 {
		t.Errorf("retried a fatal error: %d connections", f.conns.Load())
	}
}

// TestSarvamCommitsStableWordsEarly: in a long utterance, words that stop
// changing are committed before the utterance ends, leaving the last two.
func TestSarvamCommitsStableWordsEarly(t *testing.T) {
	ten := "सोई ब्रह्मा जी प्रकट हो गए रुकियो रुकियो हनुमान जी"
	s, _ := newSarvam(t, func(fc *fakeConn) {
		fc.until(BytesPerSecond / 2)
		fc.partial(ten)
		fc.untilEnd()
		fc.final(ten + " बोले एक तरफ हो जाओ।")
		fc.send(map[string]any{"event": "session.end"})
	})
	s.Early, s.StableAfter, s.EarlyAfter = true, 50*time.Millisecond, time.Millisecond
	// Audio paced over ~1 s so the 200 ms ticker sees the words stable.
	evs, err := stream(t, s, Config{Lang: "hi"}, 1, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	f := texts(evs, Final)
	// The stable words run to "रुकियो रुकियो", but the phrase ends after
	// the verb "गए": the cut is there.
	if len(f) != 2 || f[0] != "सोई ब्रह्मा जी प्रकट हो गए" || f[1] != "रुकियो रुकियो हनुमान जी बोले एक तरफ हो जाओ।" {
		t.Errorf("finals %q", f)
	}
}

func TestSarvamPingsWhenIdle(t *testing.T) {
	var pings atomic.Int32
	s, _ := newSarvam(t, func(fc *fakeConn) {
		fc.untilEnd()
		pings.Store(int32(fc.pings))
		fc.send(map[string]any{"event": "session.end"})
	})
	s.idle = 100 * time.Millisecond
	// One chunk, then a pause of about 600 ms before the next.
	if _, err := stream(t, s, Config{Lang: "hi"}, 0.2, 600*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if pings.Load() == 0 {
		t.Error("no ping during the pause")
	}
}

func TestSarvamUnsupportedLanguage(t *testing.T) {
	s := &Sarvam{Key: "k"}
	if err := s.Run(context.Background(), Config{Lang: "fr"}, nil, nil); err == nil {
		t.Error("French accepted")
	}
}

func TestKeyterms(t *testing.T) {
	var vocab []string
	for i := range 55 {
		vocab = append(vocab, strings.Repeat("क", i%5+1)+string(rune('a'+i%26)))
	}
	vocab = append([]string{strings.Repeat("y", 65), " "}, vocab...)
	kept, dropped := Keyterms(vocab)
	if len(kept) != 50 || len(dropped) != 6 {
		t.Errorf("kept %d dropped %d", len(kept), len(dropped))
	}
}
