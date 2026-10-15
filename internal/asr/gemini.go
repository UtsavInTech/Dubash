package asr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/genai"

	"github.com/UtsavInTech/dubash/internal/lang"
)

// DefaultModel is Gemini's dedicated live transcription model.
const DefaultModel = "gemini-3.5-transcribe-live"

// Live transcription sessions are capped at 10 minutes; rotate before that.
const defaultMaxSession = 9 * time.Minute

// When a session ends (operator stop, rotation, GoAway), wait at most this
// long for the transcript of the audio already sent.
const drainTimeout = 3 * time.Second

// Reconnect delays double from defaultBackoff up to maxBackoff.
const (
	defaultBackoff = time.Second
	maxBackoff     = 10 * time.Second
)

// maxConnectFailures consecutive failed connects end the Run with an
// error instead of retrying forever.
const maxConnectFailures = 5

// A session that has heard this much speech without its transcript
// moving is considered stuck and replaced. The Live model occasionally
// keeps repeating the same interim while audio flows in.
const defaultStallAfter = 8 * time.Second

// voicedRMS is the level (PCM16 RMS, about -36 dBFS) above which a chunk
// counts as speech for stall detection. Silence never looks like a stall.
const voicedRMS = 500

// Interims that end a sentence and stay unchanged this long are
// committed without waiting for the server to close the utterance.
const defaultSettleAfter = 800 * time.Millisecond

// maxBacklog bounds the audio held while no session is accepting it
// (connecting, backing off, draining); the oldest is dropped beyond it.
const maxBacklog = 10 * BytesPerSecond

// languageCodes are the BCP 47 tags Gemini Live is told to expect: Indian
// English (en-IN) and the Indian languages come from the registry.
func languageCodes(code string) []string {
	if t := lang.BCP47(code); t != "" {
		return []string{t}
	}
	return nil
}

// Gemini transcribes with the Gemini Live API. Each Run holds one session
// at a time and replaces it when it expires or fails. Transcription has no
// conversational state, so sessions are simply reopened, not resumed.
type Gemini struct {
	Client     *genai.Client
	Model      string
	MaxSession time.Duration
	Log        *slog.Logger
	Debug      bool // log every raw server message

	// Test seams: dial replaces the Live API connection, and zero
	// durations mean the defaults above.
	dial         func(ctx context.Context, cfg Config) (liveSession, error)
	firstBackoff time.Duration
	drainWait    time.Duration
	stallWait    time.Duration
	settleWait   time.Duration
}

// liveSession is the part of *genai.Session the engine uses.
type liveSession interface {
	SendRealtimeInput(genai.LiveRealtimeInput) error
	Receive() (*genai.LiveServerMessage, error)
	Close() error
}

func (g *Gemini) model() string {
	if g.Model != "" {
		return g.Model
	}
	return DefaultModel
}

func (g *Gemini) log() *slog.Logger {
	if g.Log != nil {
		return g.Log
	}
	return slog.Default()
}

func (g *Gemini) maxSession() time.Duration {
	if g.MaxSession > 0 {
		return g.MaxSession
	}
	return defaultMaxSession
}

func (g *Gemini) backoff() time.Duration {
	if g.firstBackoff > 0 {
		return g.firstBackoff
	}
	return defaultBackoff
}

func (g *Gemini) stallAfter() time.Duration {
	if g.stallWait > 0 {
		return g.stallWait
	}
	return defaultStallAfter
}

func (g *Gemini) settleAfter() time.Duration {
	if g.settleWait > 0 {
		return g.settleWait
	}
	return defaultSettleAfter
}

func (g *Gemini) drainTimeout() time.Duration {
	if g.drainWait > 0 {
		return g.drainWait
	}
	return drainTimeout
}

// connect opens one Live session. It blocks for the whole dial and setup
// handshake, whatever ctx says.
func (g *Gemini) connect(ctx context.Context, cfg Config) (liveSession, error) {
	if g.dial != nil {
		return g.dial(ctx, cfg)
	}
	sess, err := g.Client.Live.Connect(ctx, g.model(), &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityText},
		InputAudioTranscription: &genai.AudioTranscriptionConfig{
			LanguageCodes:    languageCodes(cfg.Lang),
			CustomVocabulary: cfg.Vocabulary,
		},
	})
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// rejected reports a connect failure that retrying cannot fix: the Live
// API closes the setup with 1007 (invalid argument, such as a bad API key
// or config) or 1008 (policy: model not found, permission denied).
func rejected(err error) bool {
	var ce *websocket.CloseError
	return errors.As(err, &ce) &&
		(ce.Code == websocket.CloseInvalidFramePayloadData || ce.Code == websocket.ClosePolicyViolation)
}

// Run keeps reading audio at all times, even while connecting or backing
// off, so a slow or failing API never stalls the caller. It returns nil
// once audio is closed or ctx is done, and an error when the API rejects
// the session setup or maxConnectFailures connects in a row fail.
func (g *Gemini) Run(ctx context.Context, cfg Config, audio <-chan []byte, events chan<- Event) error {
	r := &liveRun{g: g, ctx: ctx, cfg: cfg, audio: audio, events: events}
	return r.run()
}

// liveRun is the state of one Run. Only the Run goroutine touches it,
// except sent (atomic) and asm. While a session is open its receiver and
// the settle timer share asm under asmMu, which also keeps their events
// in order.
type liveRun struct {
	g      *Gemini
	ctx    context.Context // governs shutdown and every event delivery
	cfg    Config
	audio  <-chan []byte
	events chan<- Event

	sent         atomic.Int64 // bytes consumed from audio: the event clock
	asmMu        sync.Mutex
	asm          assembler // carries text across sessions so none is lost
	changed      time.Time // when the interim text last changed (under asmMu)
	backlog      [][]byte  // audio consumed but not yet sent
	backlogBytes int
	ended        bool // audio was closed

	// Audio sent to the current session since its transcript last moved,
	// replayed to the next session if this one stalls. The receiver counts
	// the transcript's moves before it emits their events, and the Run
	// goroutine compares the count with the last one it saw before it
	// counts a chunk, so a move is never counted late.
	unheard      [][]byte
	unheardBytes int
	voiced       time.Duration // speech within unheard
	moves        atomic.Int64
	seenMoves    int64
}

func (r *liveRun) done() bool { return r.ended || r.ctx.Err() != nil }

func (r *liveRun) at() float64 { return float64(r.sent.Load()) / BytesPerSecond }

func (r *liveRun) run() error {
	defer r.flush()
	backoff := r.g.backoff()
	failures := 0
	for {
		if r.ctx.Err() != nil || (r.ended && len(r.backlog) == 0) {
			return nil
		}
		sess, err := r.open()
		if sess == nil && err == nil {
			return nil // gave up connecting: ctx done or audio ended
		}
		if err == nil {
			failures = 0
			err = r.session(sess)
			r.asm.endSession() // the next session transcribes from scratch
			if err == nil {
				backoff = r.g.backoff() // rotated, replaced on GoAway, or ended
				continue
			}
		} else {
			err = fmt.Errorf("connect: %w", err)
			failures++
			if !r.done() && (rejected(err) || failures >= maxConnectFailures) {
				return err
			}
		}
		if r.done() {
			return nil
		}
		r.g.log().Warn("live session failed, reconnecting", "err", err, "in", backoff)
		send(r.ctx, r.events, Event{Kind: Error, Text: err.Error(), At: r.at()})
		if !r.sleep(backoff) {
			return nil
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// flush emits whatever text is still pending as a final. It runs when no
// receiver is alive.
func (r *liveRun) flush() {
	r.emit(r.asm.flush(), r.at())
}

// emit sends events stamped with the audio position at.
func (r *liveRun) emit(evs []Event, at float64) {
	for _, ev := range evs {
		ev.At = at
		send(r.ctx, r.events, ev)
	}
}

// hold keeps a consumed chunk for the next session.
func (r *liveRun) hold(chunk []byte) {
	r.sent.Add(int64(len(chunk)))
	r.backlog = append(r.backlog, chunk)
	r.backlogBytes += len(chunk)
	for r.backlogBytes > maxBacklog {
		r.backlogBytes -= len(r.backlog[0])
		r.backlog = r.backlog[1:]
	}
}

type dialResult struct {
	sess liveSession
	err  error
}

// open connects in the background while holding incoming audio. When
// audio ends first it still waits, up to the drain timeout, for a session
// to transcribe what it holds. It returns nil, nil when it gives up (ctx
// done, or audio ended with nothing held or no session in time); the
// connect then finishes on its own and whatever it opens is closed.
func (r *liveRun) open() (liveSession, error) {
	res := make(chan dialResult, 1)
	ctx, cancel := context.WithCancel(r.ctx)
	go func() {
		sess, err := r.g.connect(ctx, r.cfg)
		if sess == nil && err == nil {
			err = errors.New("no session")
		}
		res <- dialResult{sess, err}
	}()
	abandon := func() (liveSession, error) {
		cancel()
		select {
		case x := <-res:
			if x.sess != nil {
				x.sess.Close()
			}
		default:
			go func() {
				if x := <-res; x.sess != nil {
					x.sess.Close()
				}
			}()
		}
		return nil, nil
	}
	// Once audio has ended, held audio may wait this long for a session.
	var giveUp <-chan time.Time
	if r.ended {
		giveUp = time.After(r.g.drainTimeout())
	}
	for {
		var audio <-chan []byte
		if !r.ended {
			audio = r.audio
		}
		select {
		case x := <-res:
			cancel()
			if x.err == nil && r.ctx.Err() != nil {
				x.sess.Close()
				return nil, nil
			}
			return x.sess, x.err
		case chunk, ok := <-audio:
			if !ok {
				r.ended = true
				if len(r.backlog) == 0 {
					return abandon()
				}
				giveUp = time.After(r.g.drainTimeout())
				continue
			}
			r.hold(chunk)
		case <-giveUp:
			return abandon()
		case <-r.ctx.Done():
			return abandon()
		}
	}
}

// sleep waits d while holding incoming audio. It reports false if audio
// ended or ctx was done first.
func (r *liveRun) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			return true
		case chunk, ok := <-r.audio:
			if !ok {
				r.ended = true
				return false
			}
			r.hold(chunk)
		case <-r.ctx.Done():
			return false
		}
	}
}

// closeOnce lets a session be closed from several places, once.
type closeOnce struct {
	liveSession
	once sync.Once
	err  error
}

// Close closes the session the first time and returns that result after.
func (c *closeOnce) Close() error {
	c.once.Do(func() { c.err = c.liveSession.Close() })
	return c.err
}

func sendAudio(sess liveSession, chunk []byte) error {
	if err := sess.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{Data: chunk, MIMEType: "audio/pcm;rate=16000"},
	}); err != nil {
		return fmt.Errorf("send audio: %w", err)
	}
	return nil
}

// session streams audio into one open session until it has to end. It
// returns nil when the session was rotated, replaced on GoAway, or audio
// or ctx ended, and the error when the connection failed. Every return
// path closes sess once and takes the receiver's result once.
func (r *liveRun) session(sess liveSession) error {
	// Sends and receives on the socket have no deadlines, so cancelling
	// ctx alone would not unblock them on a stalled connection: closing
	// the session does.
	sess = &closeOnce{liveSession: sess}
	defer context.AfterFunc(r.ctx, func() { sess.Close() })()
	recvErr := make(chan error, 1)
	finished := make(chan struct{}, 1)
	goAway := make(chan time.Duration, 1)
	go func() { recvErr <- r.receive(sess, finished, goAway) }()
	closeAndWait := func() {
		sess.Close()
		<-recvErr
	}

	// Audio held while no session was open goes first, in order.
	for len(r.backlog) > 0 {
		if err := sendAudio(sess, r.backlog[0]); err != nil {
			closeAndWait()
			return err
		}
		r.backlogBytes -= len(r.backlog[0])
		r.backlog = r.backlog[1:]
	}
	r.backlog = nil
	r.forget()

	rotate := time.NewTimer(r.g.maxSession())
	defer rotate.Stop()
	settle := time.NewTicker(r.g.settleAfter() / 4)
	defer settle.Stop()
	for {
		select {
		case <-settle.C:
			r.settle()
		case <-rotate.C:
			r.drain(sess, recvErr, finished, r.g.drainTimeout())
			return nil
		case left := <-goAway:
			d := r.g.drainTimeout()
			if left > 0 && left < d {
				d = left
			}
			r.g.log().Info("live session going away, reconnecting", "timeLeft", left)
			r.drain(sess, recvErr, finished, d)
			return nil
		case err := <-recvErr:
			sess.Close()
			return err
		case <-r.ctx.Done():
			closeAndWait()
			return nil
		case chunk, ok := <-r.audio:
			if !ok {
				r.ended = true
				r.drain(sess, recvErr, finished, r.g.drainTimeout())
				return nil
			}
			r.sent.Add(int64(len(chunk)))
			if err := sendAudio(sess, chunk); err != nil {
				closeAndWait()
				return err
			}
			if m := r.moves.Load(); m != r.seenMoves {
				r.seenMoves = m
				r.forget() // the transcript moved: nothing sent so far is unheard
			}
			if r.remember(chunk) {
				msg := "transcription stalled, restarting the live session"
				r.g.log().Warn(msg, "heard", r.voiced)
				send(r.ctx, r.events, Event{Kind: Error, Text: msg, At: r.at()})
				replay := r.unheard
				r.forget()
				r.drain(sess, recvErr, finished, r.g.drainTimeout())
				r.requeue(replay)
				return nil
			}
		}
	}
}

// settle commits a finished sentence the interims have left unchanged
// for settleAfter.
func (r *liveRun) settle() {
	r.asmMu.Lock()
	defer r.asmMu.Unlock()
	if r.changed.IsZero() || time.Since(r.changed) < r.g.settleAfter() {
		return
	}
	r.changed = time.Time{}
	r.emit(r.asm.settle(), r.at())
}

// remember keeps a sent chunk until the transcript moves and reports
// whether the session has heard enough speech since then to be stuck.
func (r *liveRun) remember(chunk []byte) bool {
	r.unheard = append(r.unheard, chunk)
	r.unheardBytes += len(chunk)
	for r.unheardBytes > maxBacklog {
		r.unheardBytes -= len(r.unheard[0])
		r.unheard = r.unheard[1:]
	}
	if RMS(chunk) >= voicedRMS {
		r.voiced += time.Duration(len(chunk)) * time.Second / BytesPerSecond
	}
	return r.voiced >= r.g.stallAfter()
}

// forget drops the audio kept for a replay: the transcript moved.
func (r *liveRun) forget() {
	r.unheard, r.unheardBytes, r.voiced = nil, 0, 0
	r.seenMoves = r.moves.Load()
}

// requeue puts audio a stuck session never transcribed ahead of the
// backlog, so the next session hears it. It is already on the clock.
func (r *liveRun) requeue(chunks [][]byte) {
	r.backlog = append(chunks, r.backlog...)
	for _, c := range chunks {
		r.backlogBytes += len(c)
	}
	for r.backlogBytes > maxBacklog {
		r.backlogBytes -= len(r.backlog[0])
		r.backlog = r.backlog[1:]
	}
}

// drain ends the audio stream and gives the server up to d to transcribe
// what it already has: it stops at the first utterance end (its final or
// generationComplete), when the server closes, when d passes or when ctx
// is done. Audio arriving meanwhile is held for the next session. It closes sess and takes the
// receiver's result.
func (r *liveRun) drain(sess liveSession, recvErr <-chan error, finished <-chan struct{}, d time.Duration) {
	select {
	case <-finished: // only an utterance ended after the stream end counts
	default:
	}
	_ = sess.SendRealtimeInput(genai.LiveRealtimeInput{AudioStreamEnd: true})
	t := time.NewTimer(d)
	defer t.Stop()
	received := false
wait:
	for {
		var audio <-chan []byte
		if !r.ended {
			audio = r.audio
		}
		select {
		case <-finished:
			break wait
		case <-recvErr:
			received = true
			break wait
		case <-t.C:
			break wait
		case <-r.ctx.Done():
			break wait
		case chunk, ok := <-audio:
			if !ok {
				r.ended = true
				continue
			}
			r.hold(chunk)
		}
	}
	sess.Close()
	if !received {
		<-recvErr
	}
}

// receive turns server messages into events until the connection fails or
// is closed. It signals finished at the end of each utterance and goAway
// when the server announces the session's end, and counts in moves every
// time the transcript moves (a new interim text or a final).
func (r *liveRun) receive(sess liveSession, finished chan<- struct{}, goAway chan<- time.Duration) error {
	last := ""
	for {
		msg, err := sess.Receive()
		if err != nil {
			return fmt.Errorf("receive: %w", err)
		}
		if r.g.Debug {
			if b, err := json.Marshal(msg); err == nil {
				r.g.log().Info("live message", "msg", string(b))
			}
		}
		if msg.GoAway != nil {
			select {
			case goAway <- msg.GoAway.TimeLeft:
			default:
			}
		}
		sc := msg.ServerContent
		if sc == nil {
			continue
		}
		at := r.at()
		interim, final := sc.InterimInputTranscription, sc.InputTranscription
		if final != nil || interim != nil && interim.Text != last {
			r.moves.Add(1) // before the events go out
		}
		r.asmMu.Lock()
		if interim != nil {
			if interim.Text != last {
				r.changed = time.Now()
			}
			last = interim.Text
			r.emit(r.asm.interim(interim.Text), at)
		}
		if final != nil {
			r.emit(r.asm.final(final.Text), at)
			last = ""
			r.changed = time.Time{}
		}
		r.asmMu.Unlock()
		// An utterance ends with its inputTranscription, then
		// generationComplete. The transcription model never sends
		// turnComplete, but it would mean the same.
		if sc.InputTranscription != nil || sc.GenerationComplete || sc.TurnComplete {
			select {
			case finished <- struct{}{}:
			default:
			}
		}
	}
}
