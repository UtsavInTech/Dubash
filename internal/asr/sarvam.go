// Copyright 2026 the Dubash contributors. Apache-2.0.

package asr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/UtsavInTech/dubash/internal/lang"
)

// SarvamRealtimeURL is Sarvam's realtime speech-to-text WebSocket.
const SarvamRealtimeURL = "wss://api.sarvam.ai/speech-to-text-realtime/ws"

// DefaultSarvamModel is the only Sarvam model that takes keyterms.
const DefaultSarvamModel = "saaras:v4"

// Sarvam keyterm limits (keyterm prompting guide, read 2026-10-16).
const (
	SarvamMaxKeyterms   = 50
	SarvamMaxKeytermLen = 64
)

// Defaults for Sarvam; zero fields of Sarvam take these.
const (
	// A pause this long ends an utterance. Sarvam's own default is 1000
	// ms (observed; the docs say 500), which holds a whole paragraph of a
	// fluent speaker in one utterance.
	defaultSarvamSilence = 500 * time.Millisecond
	// Words of a partial transcript that stayed the same this long are
	// committed early once an utterance runs longer than earlyAfter.
	// Partials are rougher than finals (no punctuation, "दस" for "10",
	// sometimes a wrong word the final fixes), so early commits are kept
	// for long utterances only.
	defaultStableAfter = 1500 * time.Millisecond
	defaultEarlyAfter  = 8 * time.Second
	// Early commits take at least minEarlyWords and leave tailWords
	// uncommitted, since the last words of a partial are the ones revised.
	minEarlyWords = 6
	tailWords     = 2
	// With no audio for idlePing, the session is kept alive with pings
	// (a paused room should not be billed for streamed silence).
	idlePing = 5 * time.Second
	// After the audio ends, wait this long for the last transcript.
	sarvamDrain = 5 * time.Second
)

// Sarvam transcribes with Sarvam's realtime WebSocket (saaras:v4 by
// default): partial transcripts while the speaker talks, a punctuated
// final per utterance. Sessions are opened when audio arrives, replaced
// when they fail, and left to close when the room is paused.
//
// Observed on 2026-10-16 (cmd/sarvam-smoke): partials are cumulative for
// the utterance and unpunctuated, so sentences cannot be committed from
// them by punctuation. Sarvam commits nothing before the utterance ends;
// to keep long utterances from holding the captions back, words that stay
// unchanged across partials are committed early (see StableAfter).
type Sarvam struct {
	Key   string
	URL   string // SarvamRealtimeURL if empty
	Model string // DefaultSarvamModel if empty
	// StreamType is fast, balanced (Sarvam's default) or simulated.
	StreamType string
	// Silence ends an utterance (default 500 ms).
	Silence time.Duration
	// Early commits text from partial transcripts before the utterance
	// ends: sentences that partials already punctuate, and, in utterances
	// longer than EarlyAfter (default 8 s), words unchanged for StableAfter
	// (default 1.5 s), up to the last phrase boundary among them (see
	// phrase.go). Lines then reach the audience sooner during long
	// utterances, but partials are rougher than finals.
	Early                   bool
	StableAfter, EarlyAfter time.Duration
	Log                     *slog.Logger

	// Test seams; zero means the defaults above.
	backoff time.Duration
	drain   time.Duration
	idle    time.Duration
}

func (s *Sarvam) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func orDur(d, def time.Duration) time.Duration {
	if d != 0 {
		return d
	}
	return def
}

// Keyterms trims vocab to what Sarvam accepts: at most 50 terms of at
// most 64 characters. It returns what it kept and what it dropped.
func Keyterms(vocab []string) (kept, dropped []string) {
	for _, t := range vocab {
		t = strings.TrimSpace(t)
		switch {
		case t == "":
		case utf8.RuneCountInString(t) > SarvamMaxKeytermLen, len(kept) == SarvamMaxKeyterms:
			dropped = append(dropped, t)
		default:
			kept = append(kept, t)
		}
	}
	return kept, dropped
}

// endpoint builds the session URL for cfg.
func (s *Sarvam) endpoint(cfg Config, keyterms []string) (string, error) {
	tag := lang.SarvamRealtime(cfg.Lang)
	if tag == "" {
		return "", fmt.Errorf("sarvam: language %q is not supported", cfg.Lang)
	}
	base := s.URL
	if base == "" {
		base = SarvamRealtimeURL
	}
	model := s.Model
	if model == "" {
		model = DefaultSarvamModel
	}
	q := url.Values{
		"language_code": {tag},
		"model":         {model},
		"encoding":      {"linear16"},
		"sample_rate":   {"16000"},
		"silence_duration_ms": {fmt.Sprint(
			orDur(s.Silence, defaultSarvamSilence).Milliseconds())},
		// Utterance start and end times, so a final is placed where the
		// speaker stopped rather than where the stream was when it came.
		"return_timestamps": {"true"},
	}
	if cfg.Mode != "" {
		q.Set("mode", cfg.Mode)
	}
	if s.StreamType != "" {
		q.Set("stream_type", s.StreamType)
	}
	if cfg.Prompt != "" {
		q.Set("prompt", cfg.Prompt)
	}
	if len(keyterms) > 0 && strings.HasPrefix(model, "saaras:v4") {
		kt, _ := json.Marshal(keyterms)
		q.Set("keyterms", string(kt))
	}
	return base + "?" + q.Encode(), nil
}

// sarvamMsg is any server message; fields not used by an event are empty.
type sarvamMsg struct {
	Event     string    `json:"event"`
	Utterance int       `json:"utterance_idx"`
	Text      string    `json:"text"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	IsFatal   bool      `json:"is_fatal"`
	EndS      flexFloat `json:"end_s"` // seconds into the session's audio
}

// flexFloat decodes a number sent either as a JSON number or as a string
// (Sarvam's docs show strings, its server has sent numbers).
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	var v float64
	if _, err := fmt.Sscan(s, &v); err != nil {
		return err
	}
	*f = flexFloat(v)
	return nil
}

// errSarvamFatal wraps failures that retrying cannot fix.
type errSarvamFatal struct{ err error }

func (e errSarvamFatal) Error() string { return e.err.Error() }
func (e errSarvamFatal) Unwrap() error { return e.err }

// Run implements Engine.
func (s *Sarvam) Run(ctx context.Context, cfg Config, audio <-chan []byte, events chan<- Event) error {
	keyterms, dropped := Keyterms(cfg.Vocabulary)
	endpoint, err := s.endpoint(cfg, keyterms)
	if err != nil {
		return err
	}
	r := &sarvamRun{s: s, ctx: ctx, endpoint: endpoint, audio: audio, events: events}
	r.asm.finalsOnly = !s.Early
	if len(dropped) > 0 {
		r.emit(Event{Kind: Error, Text: fmt.Sprintf("sarvam: %d vocabulary terms not sent (at most %d terms of %d characters)", len(dropped), SarvamMaxKeyterms, SarvamMaxKeytermLen)})
	}
	return r.run()
}

// sarvamRun is the state of one Run; only its goroutine touches it.
type sarvamRun struct {
	s        *Sarvam
	ctx      context.Context
	endpoint string
	audio    <-chan []byte
	events   chan<- Event

	sent    int64    // bytes sent: the event clock
	base    float64  // the clock when the current session started
	backlog [][]byte // audio read but not yet sent
	queued  int
	ended   bool // audio was closed

	asm      assembler // carries text across sessions
	stable   stability
	uttStart time.Time // first partial of the current utterance
	last     string    // last partial text, to skip Sarvam's duplicates
}

func (r *sarvamRun) at() float64 { return float64(r.sent) / BytesPerSecond }

func (r *sarvamRun) emit(e Event) {
	if e.At == 0 {
		e.At = r.at()
	}
	send(r.ctx, r.events, e)
}

func (r *sarvamRun) emitAll(evs []Event) {
	for _, e := range evs {
		r.emit(e)
	}
}

// push queues a chunk, dropping the oldest audio beyond maxBacklog so
// captions stay live after an outage.
func (r *sarvamRun) push(chunk []byte) {
	r.backlog = append(r.backlog, chunk)
	r.queued += len(chunk)
	for r.queued > maxBacklog && len(r.backlog) > 1 {
		r.queued -= len(r.backlog[0])
		r.backlog = r.backlog[1:]
	}
}

// waitAudio blocks until audio is queued, audio ends or ctx is done.
func (r *sarvamRun) waitAudio() bool {
	for len(r.backlog) == 0 && !r.ended {
		select {
		case chunk, ok := <-r.audio:
			if !ok {
				r.ended = true
				return false
			}
			r.push(chunk)
		case <-r.ctx.Done():
			return false
		}
	}
	return len(r.backlog) > 0
}

// sleep waits d while still reading audio.
func (r *sarvamRun) sleep(d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			return
		case <-r.ctx.Done():
			return
		case chunk, ok := <-r.audio:
			if !ok {
				r.ended = true
				r.audio = nil // a nil channel blocks: wait out the timer
				continue
			}
			r.push(chunk)
		}
	}
}

func (r *sarvamRun) run() error {
	defer func() { r.emitAll(r.asm.flush()) }()
	backoff := orDur(r.s.backoff, defaultBackoff)
	failures := 0
	for {
		if !r.waitAudio() {
			return nil // nothing more to send
		}
		conn, err := r.dial()
		if err != nil {
			var fatal errSarvamFatal
			if errors.As(err, &fatal) {
				return err
			}
			failures++
			if failures >= maxConnectFailures {
				return fmt.Errorf("sarvam: %d connects failed in a row, last: %w", failures, err)
			}
			r.emit(Event{Kind: Error, Text: "sarvam: connect failed, retrying: " + err.Error()})
			r.sleep(backoff)
			backoff = min(2*backoff, maxBackoff)
			continue
		}
		failures = 0
		backoff = orDur(r.s.backoff, defaultBackoff)
		err = r.session(conn)
		r.asm.endSession()
		r.stable = stability{}
		r.last, r.uttStart = "", time.Time{}
		var fatal errSarvamFatal
		switch {
		case errors.As(err, &fatal):
			return err
		case err != nil && r.ctx.Err() == nil:
			r.emit(Event{Kind: Error, Text: "sarvam: session lost, reconnecting: " + err.Error()})
		}
		if r.ended && len(r.backlog) == 0 || r.ctx.Err() != nil {
			return nil
		}
	}
}

func (r *sarvamRun) dial() (*websocket.Conn, error) {
	dctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(dctx, r.endpoint, &websocket.DialOptions{
		HTTPHeader: http.Header{"Api-Subscription-Key": {r.s.Key}},
	})
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return nil, errSarvamFatal{fmt.Errorf("sarvam: key rejected (%s)", resp.Status)}
		}
		return nil, err
	}
	c.SetReadLimit(1 << 20)
	return c, nil
}

// session streams audio over one connection until the audio ends (nil
// after the last transcript), the connection fails (an error), or the
// server closes an idle session (nil: the next audio reopens one).
func (r *sarvamRun) session(c *websocket.Conn) error {
	defer c.CloseNow()
	msgs := make(chan sarvamMsg, 64)
	readErr := make(chan error, 1)
	quit := make(chan struct{})
	defer close(quit) // runs before CloseNow: the reader stops either way
	go func() {
		for {
			_, data, err := c.Read(r.ctx)
			if err != nil {
				readErr <- err
				return
			}
			var m sarvamMsg
			if json.Unmarshal(data, &m) == nil {
				select {
				case msgs <- m:
				case <-quit:
					return
				}
			}
		}
	}()

	r.base = r.at()
	idle := orDur(r.s.idle, idlePing)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	lastAudio := time.Now()
	lastPing := time.Now()
	var drainUntil time.Time // set once "end" was sent
	audio := r.audio
	if r.ended {
		audio = nil
	}

	for {
		// Send what is queued before reading more.
		for len(r.backlog) > 0 {
			chunk := r.backlog[0]
			if err := r.write(c, map[string]string{"event": "audio_input", "audio": base64.StdEncoding.EncodeToString(chunk)}); err != nil {
				return r.finish(err, msgs, readErr, !drainUntil.IsZero())
			}
			r.backlog = r.backlog[1:]
			r.queued -= len(chunk)
			r.sent += int64(len(chunk))
			lastAudio = time.Now()
		}
		if r.ended && drainUntil.IsZero() {
			if err := r.write(c, map[string]string{"event": "end"}); err != nil {
				return r.finish(err, msgs, readErr, true)
			}
			drainUntil = time.Now().Add(orDur(r.s.drain, sarvamDrain))
			audio = nil
		}

		select {
		case <-r.ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return nil
		case chunk, ok := <-audio:
			if !ok {
				r.ended = true
				continue
			}
			r.push(chunk)
		case m := <-msgs:
			if done, err := r.handle(m); err != nil || done {
				if done {
					c.Close(websocket.StatusNormalClosure, "")
				}
				return err
			}
			if !drainUntil.IsZero() && m.Event == "transcript.final" {
				// Sarvam does not always close after "end"; a final after
				// it is the last transcript we wait for.
				drainUntil = time.Now().Add(300 * time.Millisecond)
			}
		case err := <-readErr:
			return r.finish(err, msgs, readErr, !drainUntil.IsZero(), err)
		case now := <-tick.C:
			if !drainUntil.IsZero() && now.After(drainUntil) {
				c.Close(websocket.StatusNormalClosure, "")
				return nil
			}
			r.early(now)
			if drainUntil.IsZero() && now.Sub(lastAudio) >= idle && now.Sub(lastPing) >= idle {
				lastPing = now
				if err := r.write(c, map[string]string{"event": "ping"}); err != nil {
					return r.finish(err, msgs, readErr, false)
				}
			}
		}
	}
}

// finish ends a session that failed with err. The reader queues every
// message before it reports the close, and a write can fail before the
// reader has seen the close at all: handle what was received, waiting a
// moment for the reader, or a transcript sent just before the server hung
// up would be lost. readDone is the reader's error if already received.
func (r *sarvamRun) finish(err error, msgs <-chan sarvamMsg, readErr <-chan error, draining bool, readDone ...error) error {
	closeErr := err
	if len(readDone) == 0 {
		wait := time.NewTimer(time.Second)
		defer wait.Stop()
	reading:
		for {
			select {
			case m := <-msgs:
				if done, herr := r.handle(m); herr != nil || done {
					return herr
				}
			case rerr := <-readErr:
				closeErr = rerr
				break reading
			case <-wait.C:
				break reading
			}
		}
	}
	for {
		select {
		case m := <-msgs:
			if done, herr := r.handle(m); herr != nil || done {
				return herr
			}
		default:
			return r.closed(closeErr, draining)
		}
	}
}

func (r *sarvamRun) write(c *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
	defer cancel()
	return c.Write(wctx, websocket.MessageText, data)
}

// handle applies one server message. done reports the session ended.
func (r *sarvamRun) handle(m sarvamMsg) (done bool, err error) {
	switch m.Event {
	case "transcript.partial":
		if m.Text == r.last {
			return false, nil // Sarvam sends most partials twice
		}
		r.last = m.Text
		now := time.Now()
		if r.uttStart.IsZero() {
			r.uttStart = now
		}
		r.stable.update(strings.Fields(m.Text), now, r.at())
		r.emitAll(r.asm.interim(m.Text))
		r.early(now)
	case "transcript.final":
		at := r.at()
		if m.EndS > 0 {
			at = min(at, r.base+float64(m.EndS))
		}
		for _, e := range r.asm.final(m.Text) {
			e.At = at
			r.emit(e)
		}
		r.stable = stability{}
		r.last, r.uttStart = "", time.Time{}
	case "error":
		msg := fmt.Sprintf("sarvam: %s: %s", m.Code, m.Message)
		if m.IsFatal {
			return true, errSarvamFatal{errors.New(msg)}
		}
		r.emit(Event{Kind: Error, Text: msg})
	case "session.end":
		return true, nil
	}
	return false, nil
}

// early commits the words of a long utterance that have stopped changing.
func (r *sarvamRun) early(now time.Time) {
	after := orDur(r.s.StableAfter, defaultStableAfter)
	if !r.s.Early || after < 0 || r.uttStart.IsZero() || now.Sub(r.uttStart) < orDur(r.s.EarlyAfter, defaultEarlyAfter) {
		return
	}
	committed := len(r.asm.done)
	total := len(r.stable.words)
	limit := min(r.stable.prefix(now, after), total-tailWords)
	// Cut only where a phrase ends, as the speaker would pause: never
	// mid-phrase ("भाई, तुम क्या | कर रहे हो").
	end := lastPhraseEnd(r.stable.words, committed+minEarlyWords, limit)
	n := end - committed
	if end > 0 && n >= minEarlyWords {
		// Placed where the last committed word was first heard: commit
		// lag then reads from that moment.
		at := r.stable.at[committed+n-1]
		for _, e := range r.asm.commitWords(n) {
			if e.Kind == Final {
				e.At = at
			}
			r.emit(e)
		}
	}
}

// closed classifies the end of a connection.
func (r *sarvamRun) closed(err error, draining bool) error {
	if r.ctx.Err() != nil {
		return nil
	}
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure:
		return nil
	case websocket.StatusPolicyViolation: // 1008: inactivity timeout
		if draining {
			return nil
		}
		r.s.log().Info("sarvam closed an idle session")
		return nil
	case websocket.StatusUnsupportedData: // 1003: rate limit, quota or key
		return errSarvamFatal{fmt.Errorf("sarvam: rate limit, quota or key rejected: %w", err)}
	case 4000:
		return errSarvamFatal{fmt.Errorf("sarvam: request rejected: %w", err)}
	}
	return err
}

// stability tracks how long each word position of a partial transcript
// has held the same word.
type stability struct {
	words []string
	since []time.Time
	at    []float64 // audio clock when each word first appeared
}

func (st *stability) update(words []string, now time.Time, at float64) {
	since := make([]time.Time, len(words))
	pos := make([]float64, len(words))
	for i, w := range words {
		if i < len(st.words) && st.words[i] == w && !st.since[i].IsZero() {
			since[i], pos[i] = st.since[i], st.at[i]
		} else {
			since[i], pos[i] = now, at
		}
		// A changed word makes everything after it new as well.
		if i > 0 && since[i].Before(since[i-1]) {
			since[i], pos[i] = since[i-1], pos[i-1]
		}
	}
	st.words, st.since, st.at = words, since, pos
}

// prefix is how many leading words have been unchanged for at least d.
func (st *stability) prefix(now time.Time, d time.Duration) int {
	n := 0
	for _, t := range st.since {
		if now.Sub(t) < d {
			break
		}
		n++
	}
	return n
}
