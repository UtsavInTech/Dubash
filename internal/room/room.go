// Package room runs the captioning pipeline for one stage: operator audio →
// ASR → translation → store → viewers.
package room

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/caption"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/translate"
)

// ringSize is how many recent finals a new viewer receives.
const ringSize = 50

// translateTimeout bounds one segment's translations; segments in flight
// finish even if the operator disconnects.
const translateTimeout = 15 * time.Second

// talkGap is how long a room may stay idle and still continue the same
// talk: a reconnecting operator keeps it, the next talk after a break
// starts a new one.
const talkGap = 5 * time.Minute

// levelWindow is how long the audio meter holds its loudest reading.
const levelWindow = time.Second

var (
	// ErrBusy means another operator is already streaming into the room.
	ErrBusy = errors.New("room already has a live operator")
	// ErrLanguage means the room does not serve the requested language.
	ErrLanguage = errors.New("language not served by this room")
)

// Deps are the pipeline's collaborators, shared by every room.
type Deps struct {
	ASR        asr.Engine
	Translator translate.Translator
	Store      *store.Store
	Log        *slog.Logger
}

// Transmission describes one operator session.
type Transmission struct {
	// Lang is the language spoken on stage. Empty keeps the language of
	// the room's last transmission, initially the configured source.
	Lang string
	// Talk is the title of the talk being given. A title other than the
	// current talk's starts a new talk; empty continues the current one,
	// unless the room has been idle for longer than a break between talks.
	Talk string
}

// Status is the room's health, for the production panel.
type Status struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Source        string    `json:"source"`  // spoken in the current or last transmission
	Targets       []string  `json:"targets"` // the room's other languages
	Live          bool      `json:"live"`
	Talk          int       `json:"talk"`
	TalkTitle     string    `json:"talkTitle,omitempty"`
	Viewers       int       `json:"viewers"`
	Segments      int       `json:"segments"`
	LastSegmentAt time.Time `json:"lastSegmentAt"`
	AvgLatencyMs  int64     `json:"avgLatencyMs"`
	Errors        int       `json:"errors"`
	LastError     string    `json:"lastError,omitempty"`
	// Audio received: the loudest level of the last second (dBFS) and
	// when a chunk last arrived. LastTextAt is when the recognizer last
	// produced text. Together they tell a muted input from a stuck one.
	AudioSeconds float64   `json:"audioSeconds"`
	AudioLevel   float64   `json:"audioLevel"`
	LastAudioAt  time.Time `json:"lastAudioAt"`
	LastTextAt   time.Time `json:"lastTextAt"`
}

// Room is one stage.
type Room struct {
	cfg  config.Room
	deps Deps
	hub  *Hub

	mu        sync.Mutex
	live      bool
	speaking  string // language spoken in the current or last transmission
	talk      int    // current talk, 0 before the first
	talkTitle string
	idleSince time.Time // when the last transmission ended
	nextID    int
	lastT1    float64 // end of the last segment on the room's audio clock
	segments  int
	latency   time.Duration // sum over segments
	lastAt    time.Time
	errors    int
	lastError string
	audio     int64   // bytes received in the current transmission
	level     float64 // dBFS, loudest within levelWindow of levelAt
	levelAt   time.Time
	lastAudio time.Time
	lastText  time.Time

	// The live line is the room's interim: the finals still being
	// translated followed by the ASR's current interim. Keeping the
	// pending finals in it means a committed sentence never vanishes
	// from the screen while its translation is in flight. imu orders the
	// line's updates against the finals that shorten it.
	imu     sync.Mutex
	pending []string
	interim string
}

type finalJob struct {
	lang     string
	talk     int
	title    string
	text     string
	t0, t1   float64
	received time.Time
}

// New builds a room and restores its recent history from the store.
func New(cfg config.Room, deps Deps) (*Room, error) {
	seed, skipped, err := deps.Store.Load(cfg.ID)
	if err != nil {
		return nil, fmt.Errorf("room %s: %w", cfg.ID, err)
	}
	if skipped > 0 {
		deps.Log.Warn("skipped corrupt transcript lines", "room", cfg.ID, "lines", skipped)
	}
	r := &Room{cfg: cfg, deps: deps, hub: NewHub(ringSize, seed), speaking: cfg.Source, level: asr.DBFS(0)}
	if n := len(seed); n > 0 {
		last := seed[n-1]
		r.nextID = last.ID + 1
		r.lastT1 = last.T1
		r.talk, r.talkTitle, r.idleSince = last.Talk, last.TalkTitle, last.At
		if slices.Contains(cfg.Languages(), last.Lang) {
			r.speaking = last.Lang
		}
	}
	return r, nil
}

// Config returns the room's configuration.
func (r *Room) Config() config.Room { return r.cfg }

// Hub returns the room's fan-out to viewers.
func (r *Room) Hub() *Hub { return r.hub }

// NewTalk starts a new talk titled title (which may be empty): the lines
// transcribed from now on belong to it, including those of a transmission
// already running. It returns the new talk's number.
func (r *Room) NewTalk(title string) int {
	r.mu.Lock()
	r.newTalk(title)
	n := r.talk
	r.mu.Unlock()
	r.hub.Publish(Msg{Event: "status", Data: r.StatusEvent()})
	return n
}

// newTalk advances to the next talk. The caller holds mu.
func (r *Room) newTalk(title string) {
	r.talk++
	r.talkTitle = title
	// A talk started during a break is kept for the next transmission,
	// however long the break.
	r.idleSince = time.Time{}
}

// Run processes one operator session until audio is closed or ctx ends.
// Every language of the room other than the one spoken becomes a
// translation. Finals are published in order; Run returns after the last
// one is out.
func (r *Room) Run(ctx context.Context, t Transmission, audio <-chan []byte) error {
	r.mu.Lock()
	if r.live {
		r.mu.Unlock()
		return ErrBusy
	}
	lang := t.Lang
	if lang == "" {
		lang = r.speaking
	}
	if !slices.Contains(r.cfg.Languages(), lang) {
		r.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrLanguage, lang)
	}
	if r.talk == 0 || t.Talk != "" && t.Talk != r.talkTitle ||
		!r.idleSince.IsZero() && time.Since(r.idleSince) > talkGap {
		r.newTalk(t.Talk)
	}
	r.speaking = lang
	r.live = true
	r.audio = 0
	base := r.lastT1
	r.mu.Unlock()
	r.imu.Lock()
	r.pending, r.interim = nil, ""
	r.imu.Unlock()
	r.publishLive(true)
	defer r.publishLive(false)

	finals := make(chan finalJob, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for j := range finals {
			r.publishFinal(ctx, j)
		}
	}()

	// The meter sits between the operator and the engine; it stops with
	// Run even if the engine returned without draining the audio.
	metered := make(chan []byte)
	mctx, stopMeter := context.WithCancel(ctx)
	defer stopMeter()
	go r.meter(mctx, audio, metered)

	events := make(chan asr.Event, 64)
	asrErr := make(chan error, 1)
	go func() {
		asrErr <- r.deps.ASR.Run(ctx, asr.Config{Lang: lang, Vocabulary: r.cfg.Glossary}, metered, events)
		close(events)
	}()

	for ev := range events {
		if ev.Kind != asr.Error {
			r.mu.Lock()
			r.lastText = time.Now()
			r.mu.Unlock()
		}
		switch ev.Kind {
		case asr.Interim:
			r.imu.Lock()
			r.interim = ev.Text
			r.publishInterim()
			r.imu.Unlock()
		case asr.Final:
			// Its text is on screen already, in the last interim; the
			// interim that follows will show the rest after it.
			r.imu.Lock()
			r.pending = append(r.pending, ev.Text)
			r.interim = ""
			r.imu.Unlock()
			r.mu.Lock()
			t0 := r.lastT1
			t1 := max(base+ev.At, t0)
			r.lastT1 = t1
			job := finalJob{lang: lang, talk: r.talk, title: r.talkTitle, text: ev.Text, t0: t0, t1: t1, received: time.Now()}
			r.mu.Unlock()
			finals <- job
		case asr.Error:
			r.recordError(ev.Text)
		}
	}
	close(finals)
	<-done
	err := <-asrErr
	if err != nil {
		r.recordError(err.Error())
	}
	return err
}

// meter forwards audio to out, measuring it on the way, until audio is
// closed or ctx is done. It closes out.
func (r *Room) meter(ctx context.Context, audio <-chan []byte, out chan<- []byte) {
	defer close(out)
	for {
		var chunk []byte
		var ok bool
		select {
		case chunk, ok = <-audio:
			if !ok {
				return
			}
		case <-ctx.Done():
			return
		}
		now := time.Now()
		db := asr.DBFS(asr.RMS(chunk))
		r.mu.Lock()
		r.audio += int64(len(chunk))
		r.lastAudio = now
		if db > r.level || now.Sub(r.levelAt) > levelWindow {
			r.level, r.levelAt = db, now
		}
		r.mu.Unlock()
		select {
		case out <- chunk:
		case <-ctx.Done():
			return
		}
	}
}

func (r *Room) publishLive(live bool) {
	r.mu.Lock()
	r.live = live
	if !live {
		r.idleSince = time.Now()
	}
	r.mu.Unlock()
	r.hub.Publish(Msg{Event: "status", Data: r.StatusEvent()})
}

// StatusEvent is the viewers' "status" event: whether the room is live,
// which language is spoken (source) and translated (targets), and the
// current talk.
func (r *Room) StatusEvent() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]any{
		"live": r.live, "source": r.speaking, "targets": r.targets(r.speaking),
		"talk": r.talk, "talkTitle": r.talkTitle,
	}
}

// targets are the room's languages other than the spoken one.
func (r *Room) targets(spoken string) []string {
	return slices.DeleteFunc(r.cfg.Languages(), func(l string) bool { return l == spoken })
}

func (r *Room) publishFinal(ctx context.Context, j finalJob) {
	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), translateTimeout)
	defer cancel()
	seg := caption.Segment{
		Room: r.cfg.ID, Talk: j.talk, TalkTitle: j.title, At: j.received.UTC().Truncate(time.Millisecond),
		T0: j.t0, T1: j.t1, Lang: j.lang, Text: j.text, Translations: map[string]string{},
	}
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs []string
	)
	for _, dst := range r.targets(j.lang) {
		wg.Go(func() {
			out, err := r.deps.Translator.Translate(tctx, j.text, j.lang, dst, r.cfg.Glossary)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err.Error())
				return
			}
			seg.Translations[dst] = out
		})
	}
	wg.Wait()
	seg.Error = strings.Join(errs, "; ")

	r.mu.Lock()
	seg.ID = r.nextID
	r.nextID++
	r.segments++
	r.latency += time.Since(j.received)
	r.lastAt = time.Now()
	r.mu.Unlock()
	if seg.Error != "" {
		r.recordError(seg.Error)
	}
	if err := r.deps.Store.Append(seg); err != nil {
		r.recordError(err.Error())
	}
	// Viewers clear the live line on a final; send it again right after,
	// without this segment's text.
	r.imu.Lock()
	defer r.imu.Unlock()
	r.hub.Publish(Msg{Event: "final", Data: seg})
	if len(r.pending) > 0 {
		r.pending = r.pending[1:]
	}
	if len(r.pending) > 0 || r.interim != "" {
		r.publishInterim()
	}
}

// publishInterim sends the live line. The caller holds imu.
func (r *Room) publishInterim() {
	text := strings.Join(append(append([]string(nil), r.pending...), r.interim), " ")
	r.hub.Publish(Msg{Event: "interim", Data: map[string]string{"text": strings.TrimSpace(text)}})
}

func (r *Room) recordError(msg string) {
	r.deps.Log.Warn("room error", "room", r.cfg.ID, "err", msg)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors++
	r.lastError = msg
}

// Status snapshots the room for the production panel.
func (r *Room) Status() Status {
	viewers := r.hub.Viewers()
	r.mu.Lock()
	defer r.mu.Unlock()
	var avg int64
	if r.segments > 0 {
		avg = (r.latency / time.Duration(r.segments)).Milliseconds()
	}
	level := r.level
	if time.Since(r.levelAt) > levelWindow {
		level = asr.DBFS(0) // nothing heard lately
	}
	return Status{
		ID: r.cfg.ID, Title: r.cfg.Title, Source: r.speaking, Targets: r.targets(r.speaking),
		Live: r.live, Talk: r.talk, TalkTitle: r.talkTitle, Viewers: viewers,
		Segments: r.segments, LastSegmentAt: r.lastAt, AvgLatencyMs: avg,
		Errors: r.errors, LastError: r.lastError,
		AudioSeconds: float64(r.audio) / asr.BytesPerSecond, AudioLevel: level, LastAudioAt: r.lastAudio, LastTextAt: r.lastText,
	}
}
