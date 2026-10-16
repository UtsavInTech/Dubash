// Package room runs the captioning pipeline for one stage: operator audio →
// ASR → translation → viewers, with the transcript stored on the side.
//
// Modified substantially by the Dubash contributors: committed lines are
// published at once and translated per language, by one worker per
// language and only while someone reads it; storage is asynchronous;
// transmissions can be paused and the event ended.
package room

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/caption"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/translate"
)

// ringSize is how many recent segments a new viewer receives.
const ringSize = 50

// translateTimeout bounds one translation; translations in flight finish
// even if the operator disconnects.
const translateTimeout = 15 * time.Second

// langQueue is how many lines may wait for one language's translator. A
// language that falls further behind (a rate limit, a slow provider)
// skips lines instead of delaying the others.
const langQueue = 32

// maxBatch is the most lines translated in one request, when several are
// waiting (an utterance commits all its sentences at once). With
// translate.Batcher providers this cut requests, and so rate-limit waits;
// the neighbouring lines also give the translator context.
const maxBatch = 8

// rateLimitRetry is how long a rate-limited request waits before its one
// retry.
const rateLimitRetry = 2 * time.Second

// talkGap is how long a room may stay idle and still continue the same
// talk: a reconnecting operator keeps it, the next talk after a break
// starts a new one.
const talkGap = 5 * time.Minute

// levelWindow is how long the audio meter holds its loudest reading.
const levelWindow = time.Second

// samples kept per latency measure, for percentiles.
const samples = 256

var (
	// ErrBusy means another operator is already streaming into the room.
	ErrBusy = errors.New("room already has a live operator")
	// ErrLanguage means the room does not serve the requested language.
	ErrLanguage = errors.New("language not served by this room")
	// ErrEnded means the event was ended from the panel.
	ErrEnded = errors.New("the event was ended")
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

// Latency summarizes recent measurements, in milliseconds.
type Latency struct {
	P50 int64 `json:"p50"`
	P95 int64 `json:"p95"`
	N   int   `json:"n"`
}

// Status is the room's health, for the production panel.
type Status struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Speaker       string    `json:"speaker,omitempty"`
	Source        string    `json:"source"`  // spoken in the current or last transmission
	Targets       []string  `json:"targets"` // the room's other languages
	Live          bool      `json:"live"`
	Paused        bool      `json:"paused"`
	Ended         bool      `json:"ended"`
	Epoch         int       `json:"epoch"`
	Talk          int       `json:"talk"`
	TalkTitle     string    `json:"talkTitle,omitempty"`
	Viewers       int       `json:"viewers"`
	Segments      int       `json:"segments"`
	LastSegmentAt time.Time `json:"lastSegmentAt"`
	// AvgLatencyMs is the mean time from a committed line to its last
	// translation (kept for the upstream panel and metric).
	AvgLatencyMs int64  `json:"avgLatencyMs"`
	Errors       int    `json:"errors"`
	LastError    string `json:"lastError,omitempty"`
	// Audio received: the loudest level of the last second (dBFS) and
	// when a chunk last arrived. LastTextAt is when the recognizer last
	// produced text. Together they tell a muted input from a stuck one.
	AudioSeconds float64   `json:"audioSeconds"`
	AudioLevel   float64   `json:"audioLevel"`
	LastAudioAt  time.Time `json:"lastAudioAt"`
	LastTextAt   time.Time `json:"lastTextAt"`
	// Listeners counts viewers per language they read.
	Listeners map[string]int `json:"listeners"`
	// CommitLag is how far behind the audio a line is committed (seconds
	// of audio received after its end), per line.
	CommitLag Latency `json:"commitLag"`
	// Translation is the time from a committed line to its translation,
	// per language.
	Translation map[string]Latency `json:"translation"`
	// FirstCaptionMs is the time from the start of the transmission to
	// its first committed line.
	FirstCaptionMs int64 `json:"firstCaptionMs"`
	// Dropped counts lines a language skipped because it fell behind.
	Dropped map[string]int `json:"dropped,omitempty"`
}

// Room is one stage.
type Room struct {
	cfg  config.Room
	deps Deps
	hub  *Hub

	mu        sync.Mutex
	live      bool
	paused    bool
	ended     bool
	stop      context.CancelFunc // cancels the live transmission
	epoch     int
	speaking  string // language spoken in the current or last transmission
	talk      int    // current talk, 0 before the first
	talkTitle string
	idleSince time.Time // when the last transmission ended
	nextID    int
	lastT1    float64 // end of the last segment on the room's audio clock
	segments  int
	latency   time.Duration // sum over translated segments
	latencyN  int
	lastAt    time.Time
	errors    int
	lastError string
	audio     int64   // bytes received in the current transmission
	level     float64 // dBFS, loudest within levelWindow of levelAt
	levelAt   time.Time
	lastAudio time.Time
	lastText  time.Time
	started   time.Time
	firstCap  time.Duration
	commitLag []time.Duration
	trLat     map[string][]time.Duration
	dropped   map[string]int
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
	r := &Room{
		cfg: cfg, deps: deps, hub: NewHub(ringSize, seed), speaking: cfg.Source, level: asr.DBFS(0),
		trLat: map[string][]time.Duration{}, dropped: map[string]int{},
	}
	if n := len(seed); n > 0 {
		last := seed[n-1]
		for _, s := range seed {
			r.nextID = max(r.nextID, s.ID+1)
			r.epoch = max(r.epoch, s.Epoch)
		}
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
	r.publishStatus()
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

// Pause stops sending the live audio to the recognizer: nothing said while
// paused is transcribed, translated, stored or billed. The transmission
// stays connected. Resume undoes it.
func (r *Room) Pause() { r.setPaused(true) }

// Resume restarts recognition after Pause.
func (r *Room) Resume() { r.setPaused(false) }

func (r *Room) setPaused(p bool) {
	r.mu.Lock()
	changed := r.paused != p
	r.paused = p
	r.mu.Unlock()
	if changed {
		r.publishStatus()
	}
}

// End ends the event: the live transmission, if any, stops, and viewers
// see that the event is over until the next transmission starts.
func (r *Room) End() {
	r.mu.Lock()
	r.ended = true
	r.paused = false
	stop := r.stop
	r.mu.Unlock()
	if stop != nil {
		stop()
	}
	r.publishStatus()
}

// Run processes one operator session until audio is closed or ctx ends.
// Every language of the room other than the one spoken becomes a
// translation. Run returns after the last line is published and stored.
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
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	r.stop = stop
	r.speaking = lang
	r.live, r.paused, r.ended = true, false, false
	r.epoch++
	r.audio = 0
	r.started, r.firstCap = time.Now(), 0
	base := r.lastT1
	epoch := r.epoch
	r.mu.Unlock()
	r.publishStatus()
	defer func() {
		r.mu.Lock()
		r.live, r.paused, r.stop = false, false, nil
		r.idleSince = time.Now()
		r.mu.Unlock()
		r.publishStatus()
	}()

	p := newPipeline(r, lang, epoch)

	// The meter sits between the operator and the engine; it stops with
	// Run even if the engine returned without draining the audio.
	metered := make(chan []byte)
	mctx, stopMeter := context.WithCancel(ctx)
	defer stopMeter()
	go r.meter(mctx, audio, metered)

	events := make(chan asr.Event, 64)
	asrErr := make(chan error, 1)
	go func() {
		asrErr <- r.deps.ASR.Run(ctx, asr.Config{
			Lang: lang, Vocabulary: r.cfg.Glossary, Mode: r.cfg.ASRMode, Prompt: r.cfg.Prompt,
		}, metered, events)
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
			r.hub.Publish(Msg{Event: "interim", Data: caption.Interim{Epoch: epoch, Lang: lang, Text: ev.Text}})
		case asr.Final:
			r.mu.Lock()
			t0 := r.lastT1
			t1 := max(base+ev.At, t0)
			r.lastT1 = t1
			seg := caption.Segment{
				ID: r.nextID, Room: r.cfg.ID, Epoch: epoch, Talk: r.talk, TalkTitle: r.talkTitle,
				At: time.Now().UTC().Truncate(time.Millisecond), T0: t0, T1: t1, Lang: lang, Text: ev.Text,
			}
			r.nextID++
			r.segments++
			r.lastAt = time.Now()
			if r.firstCap == 0 {
				r.firstCap = time.Since(r.started)
			}
			// How much audio arrived after the line's end: its commit lag.
			received := float64(r.audio) / asr.BytesPerSecond
			r.commitLag = keep(r.commitLag, time.Duration((received-ev.At)*float64(time.Second)))
			r.mu.Unlock()
			p.commit(ctx, seg)
		case asr.Error:
			r.recordError(ev.Text)
		}
	}
	p.close()
	err := <-asrErr
	if err != nil {
		r.recordError(err.Error())
	}
	r.mu.Lock()
	ended := r.ended
	r.mu.Unlock()
	if ended && (err == nil || errors.Is(err, context.Canceled)) {
		return ErrEnded
	}
	return err
}

func keep(s []time.Duration, d time.Duration) []time.Duration {
	s = append(s, max(d, 0))
	if len(s) > samples {
		s = s[len(s)-samples:]
	}
	return s
}

// meter forwards audio to out, measuring it on the way, until audio is
// closed or ctx is done. It closes out. While the room is paused audio is
// measured (the operator still sees the level) but not forwarded.
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
		paused := r.paused
		if !paused {
			r.audio += int64(len(chunk))
		}
		r.lastAudio = now
		if db > r.level || now.Sub(r.levelAt) > levelWindow {
			r.level, r.levelAt = db, now
		}
		r.mu.Unlock()
		if paused {
			continue
		}
		select {
		case out <- chunk:
		case <-ctx.Done():
			return
		}
	}
}

func (r *Room) publishStatus() {
	r.hub.Publish(Msg{Event: "status", Data: r.StatusEvent()})
}

// StatusEvent is the viewers' "status" event: whether the room is live,
// paused or ended, which language is spoken (source) and translated
// (targets), and the current talk.
func (r *Room) StatusEvent() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]any{
		"live": r.live, "paused": r.paused, "ended": r.ended, "epoch": r.epoch,
		"source": r.speaking, "targets": r.targets(r.speaking),
		"talk": r.talk, "talkTitle": r.talkTitle,
	}
}

// targets are the room's languages other than the spoken one.
func (r *Room) targets(spoken string) []string {
	return slices.DeleteFunc(r.cfg.Languages(), func(l string) bool { return l == spoken })
}

func (r *Room) recordError(msg string) {
	r.deps.Log.Warn("room error", "room", r.cfg.ID, "err", msg)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors++
	r.lastError = msg
}

func summarize(ds []time.Duration) Latency {
	if len(ds) == 0 {
		return Latency{}
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	at := func(p float64) int64 {
		i := int(float64(len(s))*p/100+0.5) - 1
		return s[max(0, min(i, len(s)-1))].Milliseconds()
	}
	return Latency{P50: at(50), P95: at(95), N: len(s)}
}

// Status snapshots the room for the production panel.
func (r *Room) Status() Status {
	viewers := r.hub.Viewers()
	listeners := r.hub.Listeners()
	r.mu.Lock()
	defer r.mu.Unlock()
	var avg int64
	if r.latencyN > 0 {
		avg = (r.latency / time.Duration(r.latencyN)).Milliseconds()
	}
	level := r.level
	if time.Since(r.levelAt) > levelWindow {
		level = asr.DBFS(0) // nothing heard lately
	}
	tr := map[string]Latency{}
	for l, ds := range r.trLat {
		tr[l] = summarize(ds)
	}
	dropped := map[string]int{}
	for l, n := range r.dropped {
		dropped[l] = n
	}
	return Status{
		ID: r.cfg.ID, Title: r.cfg.Title, Speaker: r.cfg.Speaker, Source: r.speaking, Targets: r.targets(r.speaking),
		Live: r.live, Paused: r.paused, Ended: r.ended, Epoch: r.epoch,
		Talk: r.talk, TalkTitle: r.talkTitle, Viewers: viewers,
		Segments: r.segments, LastSegmentAt: r.lastAt, AvgLatencyMs: avg,
		Errors: r.errors, LastError: r.lastError,
		AudioSeconds: float64(r.audio) / asr.BytesPerSecond, AudioLevel: level, LastAudioAt: r.lastAudio, LastTextAt: r.lastText,
		Listeners: listeners, CommitLag: summarize(r.commitLag), Translation: tr,
		FirstCaptionMs: r.firstCap.Milliseconds(), Dropped: dropped,
	}
}

// pipeline is the translation and storage side of one transmission: a
// worker per target language, started when a line first needs it, and a
// writer that stores each segment once its translations are in.
type pipeline struct {
	r       *Room
	lang    string
	epoch   int
	targets []string

	mu      sync.Mutex
	workers map[string]chan job
	wg      sync.WaitGroup

	results chan result
	stored  chan struct{}
}

type job struct {
	seg      caption.Segment
	received time.Time
}

type result struct {
	seg    *caption.Segment // a new segment and the languages it waits for
	expect []string
	id     int // a translation: segment, language, text or error
	lang   string
	text   string
	err    string
}

func newPipeline(r *Room, lang string, epoch int) *pipeline {
	p := &pipeline{
		r: r, lang: lang, epoch: epoch, targets: r.targets(lang),
		workers: map[string]chan job{}, results: make(chan result, 256), stored: make(chan struct{}),
	}
	go p.store()
	return p
}

// active are the targets to translate now: those someone reads, and those
// the room always translates.
func (p *pipeline) active() []string {
	var out []string
	for _, l := range p.targets {
		if slices.Contains(p.r.cfg.AlwaysTranslate, l) || p.r.hub.Reading(l) {
			out = append(out, l)
		}
	}
	return out
}

// commit publishes a committed line and queues its translations.
func (p *pipeline) commit(ctx context.Context, seg caption.Segment) {
	p.r.hub.Publish(Msg{Event: "final", Data: seg, Segment: seg.ID})
	langs := p.active()
	p.results <- result{seg: &seg, expect: langs}
	for _, l := range langs {
		select {
		case p.worker(ctx, l) <- job{seg: seg, received: time.Now()}:
		default:
			// This language is too far behind: skip the line for it
			// rather than hold everything else back.
			p.r.mu.Lock()
			p.r.dropped[l]++
			p.r.mu.Unlock()
			msg := fmt.Sprintf("translation into %s is falling behind: line %d skipped", l, seg.ID)
			p.r.recordError(msg)
			p.results <- result{id: seg.ID, lang: l, err: msg}
		}
	}
}

// worker returns the job queue of lang, starting its worker.
func (p *pipeline) worker(ctx context.Context, lang string) chan job {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ch, ok := p.workers[lang]; ok {
		return ch
	}
	ch := make(chan job, langQueue)
	p.workers[lang] = ch
	p.wg.Go(func() {
		var next *job
		for {
			var batch []job
			if next != nil {
				batch, next = []job{*next}, nil
			} else if j, ok := <-ch; ok {
				batch = []job{j}
			} else {
				return
			}
			// Take the lines already waiting, within the batch limits.
			chars := len([]rune(batch[0].seg.Text))
		gather:
			for len(batch) < maxBatch {
				select {
				case j, ok := <-ch:
					if !ok {
						break gather
					}
					if chars += len([]rune(j.seg.Text)) + 1; chars > translate.MaxBatchChars {
						next = &j
						break gather
					}
					batch = append(batch, j)
				default:
					break gather
				}
			}
			p.translate(ctx, lang, batch)
		}
	})
	return ch
}

// translate translates a batch of consecutive lines into lang, in one
// request when the provider can, and publishes each line's translation in
// order.
func (p *pipeline) translate(ctx context.Context, lang string, batch []job) {
	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), translateTimeout)
	defer cancel()
	src := batch[0].seg.Lang
	outs := make([]string, len(batch))
	errs := make([]error, len(batch))
	call := func(f func() error) error {
		err := f()
		if errors.Is(err, translate.ErrRateLimited) {
			select {
			case <-time.After(rateLimitRetry):
				err = f()
			case <-tctx.Done():
			}
		}
		return err
	}
	b, batched := p.r.deps.Translator.(translate.Batcher)
	done := false
	if batched && len(batch) > 1 {
		lines := make([]string, len(batch))
		for i, j := range batch {
			lines[i] = j.seg.Text
		}
		var got []string
		err := call(func() (err error) {
			got, err = b.TranslateBatch(tctx, lines, src, lang, p.r.cfg.Glossary)
			return err
		})
		switch {
		case err == nil:
			copy(outs, got)
			done = true
		case !errors.Is(err, translate.ErrBatchSplit):
			for i := range errs {
				errs[i] = err
			}
			done = true
		}
	}
	if !done { // one by one
		for i, j := range batch {
			errs[i] = call(func() (err error) {
				outs[i], err = p.r.deps.Translator.Translate(tctx, j.seg.Text, src, lang, p.r.cfg.Glossary)
				return err
			})
		}
	}
	for i, j := range batch {
		tr := caption.Translation{Room: j.seg.Room, Segment: j.seg.ID, Epoch: j.seg.Epoch, Rev: j.seg.Rev, Lang: lang, Text: outs[i]}
		res := result{id: j.seg.ID, lang: lang, text: outs[i]}
		if errs[i] != nil {
			tr.Text, res.text = "", ""
			tr.Error = errs[i].Error()
			res.err = errs[i].Error()
			p.r.recordError(errs[i].Error())
		}
		p.r.hub.Publish(Msg{Event: "translation", Lang: lang, Data: tr, Segment: j.seg.ID})
		took := time.Since(j.received)
		p.r.mu.Lock()
		p.r.trLat[lang] = keep(p.r.trLat[lang], took)
		p.r.latency += took
		p.r.latencyN++
		p.r.mu.Unlock()
		p.results <- res
	}
}

// close waits for the translations in flight and the last stores.
func (p *pipeline) close() {
	p.mu.Lock()
	for _, ch := range p.workers {
		close(ch)
	}
	p.mu.Unlock()
	p.wg.Wait()
	close(p.results)
	<-p.stored
}

// store writes segments in order, each once its translations are in, so
// the live path never waits for the disk.
func (p *pipeline) store() {
	defer close(p.stored)
	type pending struct {
		seg  caption.Segment
		want map[string]bool
		errs []string
	}
	var queue []*pending
	byID := map[int]*pending{}
	flush := func(all bool) {
		for len(queue) > 0 && (all || len(queue[0].want) == 0) {
			q := queue[0]
			queue = queue[1:]
			delete(byID, q.seg.ID)
			sort.Strings(q.errs)
			q.seg.Error = strings.Join(q.errs, "; ")
			if err := p.r.deps.Store.Append(q.seg); err != nil {
				p.r.recordError(err.Error())
			}
		}
	}
	for res := range p.results {
		if res.seg != nil {
			q := &pending{seg: *res.seg, want: map[string]bool{}}
			q.seg.Translations = map[string]string{}
			for _, l := range res.expect {
				q.want[l] = true
			}
			queue = append(queue, q)
			byID[q.seg.ID] = q
		} else if q, ok := byID[res.id]; ok {
			delete(q.want, res.lang)
			if res.err != "" {
				q.errs = append(q.errs, res.err)
			} else {
				q.seg.Translations[res.lang] = res.text
			}
		}
		flush(false)
	}
	flush(true)
}
