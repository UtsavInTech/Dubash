// Modified by the Dubash contributors: translations travel as their own
// messages, only into languages someone reads; pause and end.

package room

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/bank"
	"github.com/UtsavInTech/dubash/internal/caption"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/translate"
)

type failTranslator struct{}

func (failTranslator) Translate(context.Context, string, string, string, translate.Glossary) (string, error) {
	return "", errors.New("boom")
}

func newRoomWith(t *testing.T, cfg config.Room, st *store.Store, engine asr.Engine, tr translate.Translator) *Room {
	t.Helper()
	rm, err := New(cfg, Deps{ASR: engine, Translator: tr, Store: st, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	return rm
}

func newRoom(t *testing.T, st *store.Store, tr translate.Translator) *Room {
	t.Helper()
	return newRoomWith(t, config.Room{ID: "sala-a", Title: "A", Source: "en", Targets: []string{"es"}}, st, asr.Fake{Every: 1}, tr)
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// feed sends secs seconds of silence in 100 ms chunks, then closes audio.
func feed(audio chan<- []byte, secs int) {
	for range secs * 10 {
		audio <- make([]byte, asr.BytesPerSecond/10)
	}
	close(audio)
}

// finals drains sub and returns the committed lines, with the
// translations that arrived for them merged in (and their errors).
func finals(sub <-chan Msg) []caption.Segment {
	var out []caption.Segment
	index := map[int]int{}
	for len(sub) > 0 {
		m := <-sub
		switch d := m.Data.(type) {
		case caption.Segment:
			if m.Event != "final" {
				continue
			}
			d.Translations = map[string]string{}
			index[d.ID] = len(out)
			out = append(out, d)
		case caption.Translation:
			if i, ok := index[d.Segment]; ok {
				if d.Error != "" {
					out[i].Error = d.Error
				} else {
					out[i].Translations[d.Lang] = d.Text
				}
			}
		}
	}
	return out
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRoomPublishesTranslatedFinals(t *testing.T) {
	st := newStore(t)
	rm := newRoom(t, st, translate.Fake{})
	sub, _, cancel := rm.Hub().Subscribe(64, "es", "")
	defer cancel()

	audio := make(chan []byte)
	go feed(audio, 2)
	if err := rm.Run(context.Background(), Transmission{}, audio); err != nil {
		t.Fatal(err)
	}

	got := finals(sub)
	if len(got) != 2 {
		t.Fatalf("finals = %d, want 2", len(got))
	}
	first := got[0]
	if first.ID != 0 || first.Room != "sala-a" || first.Lang != "en" || first.T0 != 0 || first.T1 != 1 || first.Epoch != 1 ||
		first.Text != "Welcome to Nerdearla." || first.Translations["es"] != "[es] Welcome to Nerdearla." {
		t.Fatalf("first = %+v", first)
	}
	if got[1].ID != 1 || got[1].T0 != 1 || got[1].T1 != 2 {
		t.Fatalf("second = %+v", got[1])
	}
	stored, _, _ := st.Load("sala-a")
	if len(stored) != 2 || stored[0].Translations["es"] != "[es] Welcome to Nerdearla." {
		t.Fatalf("stored = %+v", stored)
	}
	s := rm.Status()
	if s.Live || s.Segments != 2 || s.Errors != 0 || s.LastSegmentAt.IsZero() || s.Translation["es"].N != 2 {
		t.Fatalf("status = %+v", s)
	}
}

func TestRoomRejectsSecondOperator(t *testing.T) {
	rm := newRoom(t, newStore(t), translate.Fake{})
	first := make(chan []byte)
	done := make(chan error, 1)
	go func() { done <- rm.Run(context.Background(), Transmission{}, first) }()
	waitFor(t, func() bool { return rm.Status().Live })

	if err := rm.Run(context.Background(), Transmission{}, make(chan []byte)); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	close(first)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRoomKeepsOriginalWhenTranslationFails(t *testing.T) {
	st := newStore(t)
	rm := newRoom(t, st, failTranslator{})
	sub, _, cancel := rm.Hub().Subscribe(64, "es", "")
	defer cancel()
	audio := make(chan []byte)
	go feed(audio, 2)
	_ = rm.Run(context.Background(), Transmission{}, audio)

	got := finals(sub)
	if len(got) != 2 || got[0].Error == "" || got[0].Translations["es"] != "" || got[0].Text == "" {
		t.Fatalf("finals = %+v", got)
	}
	if s := rm.Status(); s.Errors != 2 || s.LastError == "" {
		t.Fatalf("status = %+v", s)
	}
	if stored, _, _ := st.Load("sala-a"); len(stored) != 2 || stored[0].Error == "" || stored[0].Text == "" {
		t.Fatalf("stored = %+v", stored)
	}
}

// counting counts translations per target language.
type counting struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *counting) Translate(_ context.Context, text, _, dst string, _ translate.Glossary) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	c.n[dst]++
	return "[" + dst + "] " + text, nil
}

// TestRoomTranslatesOnlyLanguagesBeingRead: audience size never multiplies
// translations, and a language nobody reads costs nothing.
func TestRoomTranslatesOnlyLanguagesBeingRead(t *testing.T) {
	cfg := config.Room{ID: "hall", Source: "hi", Targets: []string{"en", "te", "kn"}, AlwaysTranslate: []string{"en"}}
	c := &counting{}
	st := newStore(t)
	rm := newRoomWith(t, cfg, st, asr.Fake{Every: 1, Lines: map[string][]string{"hi": {"नमस्ते।", "धन्यवाद।"}}}, c)
	for range 3 { // three Telugu readers
		_, _, cancel := rm.Hub().Subscribe(64, "te", "")
		defer cancel()
	}
	audio := make(chan []byte)
	go feed(audio, 2)
	if err := rm.Run(context.Background(), Transmission{}, audio); err != nil {
		t.Fatal(err)
	}
	if c.n["te"] != 2 || c.n["en"] != 2 || c.n["kn"] != 0 {
		t.Fatalf("translations per language %v: want te 2 (once per line, not per reader), en 2 (always), kn 0", c.n)
	}
	stored, _, _ := st.Load("hall")
	if len(stored) != 2 || len(stored[0].Translations) != 2 {
		t.Fatalf("stored %+v", stored)
	}
	if l := rm.Status().Listeners; l["te"] != 3 {
		t.Fatalf("listeners %v", l)
	}
}

// slowLang delays one language and fails another.
type slowLang struct{ delay time.Duration }

func (s slowLang) Translate(ctx context.Context, text, _, dst string, _ translate.Glossary) (string, error) {
	switch dst {
	case "te":
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	case "kn":
		return "", errors.New("kn provider down")
	}
	return "[" + dst + "] " + text, nil
}

// TestRoomIsolatesLanguages: a slow language and a failing one do not
// hold back the source lines or the other language, and each language
// stays in order.
func TestRoomIsolatesLanguages(t *testing.T) {
	cfg := config.Room{ID: "hall", Source: "hi", Targets: []string{"en", "te", "kn"}}
	lines := []string{"एक।", "दो।", "तीन।", "चार।"}
	st := newStore(t)
	rm := newRoomWith(t, cfg, st, asr.Fake{Every: 0.5, Lines: map[string][]string{"hi": lines}}, slowLang{delay: 300 * time.Millisecond})
	en, _, c1 := rm.Hub().Subscribe(64, "en", "")
	defer c1()
	te, _, c2 := rm.Hub().Subscribe(64, "te", "")
	defer c2()
	kn, _, c3 := rm.Hub().Subscribe(64, "kn", "")
	defer c3()

	audio := make(chan []byte)
	go feed(audio, 2)
	start := time.Now()
	if err := rm.Run(context.Background(), Transmission{}, audio); err != nil {
		t.Fatal(err)
	}
	_ = start

	order := func(sub <-chan Msg, lang string) (sources, translated []int, errs int) {
		for len(sub) > 0 {
			m := <-sub
			switch d := m.Data.(type) {
			case caption.Segment:
				sources = append(sources, d.ID)
			case caption.Translation:
				if d.Lang != lang {
					t.Errorf("%s reader got %s", lang, d.Lang)
				}
				if d.Error != "" {
					errs++
				} else {
					translated = append(translated, d.Segment)
				}
			}
		}
		return
	}
	src, enDone, _ := order(en, "en")
	_, teDone, _ := order(te, "te")
	_, knDone, knErrs := order(kn, "kn")
	if !slices.Equal(src, []int{0, 1, 2, 3}) || !slices.Equal(enDone, src) || !slices.Equal(teDone, src) {
		t.Fatalf("sources %v, en %v, te %v: every language complete and in order", src, enDone, teDone)
	}
	if len(knDone) != 0 || knErrs != 4 {
		t.Fatalf("kn: %v done, %d errors", knDone, knErrs)
	}
	stored, _, _ := st.Load("hall")
	if len(stored) != 4 || stored[3].Translations["te"] == "" || stored[3].Translations["en"] == "" || !strings.Contains(stored[3].Error, "kn provider down") {
		t.Fatalf("stored %+v", stored)
	}
	for i, s := range stored {
		if s.ID != i {
			t.Fatalf("stored out of order: %+v", stored)
		}
	}
}

func TestRoomResumesClockAndIDsFromStore(t *testing.T) {
	st := newStore(t)
	rm := newRoom(t, st, translate.Fake{})
	audio := make(chan []byte)
	go feed(audio, 2)
	_ = rm.Run(context.Background(), Transmission{}, audio)

	again := newRoom(t, st, translate.Fake{})
	if n := len(again.Hub().Recent()); n != 2 {
		t.Fatalf("recent after reload = %d, want 2", n)
	}
	sub, _, cancel := again.Hub().Subscribe(64, "", "")
	defer cancel()
	audio = make(chan []byte)
	go feed(audio, 1)
	_ = again.Run(context.Background(), Transmission{}, audio)
	got := finals(sub)
	if len(got) != 1 || got[0].ID != 2 || got[0].T0 != 2 || got[0].T1 != 3 || got[0].Epoch != 2 {
		t.Fatalf("after reload = %+v", got)
	}
}

// scripted emits fixed events, then waits for audio to end.
type scripted []asr.Event

func (s scripted) Run(ctx context.Context, _ asr.Config, audio <-chan []byte, events chan<- asr.Event) error {
	for _, ev := range s {
		events <- ev
	}
	for range audio {
	}
	return nil
}

// gated translates only when released.
type gated chan struct{}

func (g gated) Translate(ctx context.Context, text, _, dst string, _ translate.Glossary) (string, error) {
	<-g
	return "[" + dst + "] " + text, nil
}

// TestRoomPublishesSourceLineBeforeTranslation: a committed line reaches
// readers of the original at once, not after the slowest translation.
func TestRoomPublishesSourceLineBeforeTranslation(t *testing.T) {
	release := make(gated)
	rm := newRoomWith(t, config.Room{ID: "sala-a", Title: "A", Source: "en", Targets: []string{"es"}}, newStore(t),
		scripted{
			{Kind: asr.Interim, Text: "Welcome to Nerdearla. Today"},
			{Kind: asr.Final, Text: "Welcome to Nerdearla."},
			{Kind: asr.Interim, Text: "Today, we"},
		}, release)
	sub, _, cancel := rm.Hub().Subscribe(64, "es", "")
	defer cancel()
	audio := make(chan []byte)
	done := make(chan error, 1)
	go func() { done <- rm.Run(context.Background(), Transmission{}, audio) }()

	next := func() Msg {
		t.Helper()
		for {
			select {
			case m := <-sub:
				if m.Event != "status" {
					return m
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no message")
			}
		}
	}
	text := func(m Msg) string { return m.Data.(caption.Interim).Text }

	if m := next(); m.Event != "interim" || text(m) != "Welcome to Nerdearla. Today" {
		t.Fatalf("got %+v", m)
	}
	if m := next(); m.Event != "final" || m.Data.(caption.Segment).Text != "Welcome to Nerdearla." || m.Seq == 0 {
		t.Fatalf("got %+v", m)
	}
	if m := next(); m.Event != "interim" || text(m) != "Today, we" {
		t.Fatalf("got %+v", m)
	}
	release <- struct{}{}
	if m := next(); m.Event != "translation" || m.Data.(caption.Translation).Text != "[es] Welcome to Nerdearla." {
		t.Fatalf("got %+v", m)
	}
	close(audio)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// heard records the audio an engine receives.
type heard struct{ bytes atomic.Int64 }

func (h *heard) Run(ctx context.Context, _ asr.Config, audio <-chan []byte, _ chan<- asr.Event) error {
	for chunk := range audio {
		h.bytes.Add(int64(len(chunk)))
	}
	return nil
}

// TestPauseKeepsAudioFromTheRecognizer: nothing said while paused is
// transcribed or billed.
func TestPauseKeepsAudioFromTheRecognizer(t *testing.T) {
	h := &heard{}
	rm := newRoomWith(t, config.Room{ID: "a", Source: "hi", Targets: []string{"en"}}, newStore(t), h, translate.Fake{})
	sub, _, cancel := rm.Hub().Subscribe(64, "", "")
	defer cancel()
	audio := make(chan []byte)
	done := make(chan error, 1)
	go func() { done <- rm.Run(context.Background(), Transmission{}, audio) }()
	chunk := make([]byte, asr.BytesPerSecond/10)
	sent := time.Now()
	audio <- chunk
	// Pause only once the meter has looked at the first chunk.
	waitFor(t, func() bool { return !rm.Status().LastAudioAt.Before(sent) })
	rm.Pause()
	for range 5 {
		sent = time.Now()
		audio <- chunk
	}
	// Resume only once the meter has looked at the last paused chunk.
	waitFor(t, func() bool { return !rm.Status().LastAudioAt.Before(sent) })
	if st := rm.Status(); !st.Paused || !st.Live {
		t.Fatalf("status %+v", st)
	}
	rm.Resume()
	audio <- chunk
	close(audio)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := h.bytes.Load(); got != int64(2*len(chunk)) {
		t.Fatalf("recognizer heard %d bytes, want %d", got, 2*len(chunk))
	}
	var paused bool
	for len(sub) > 0 {
		if m := <-sub; m.Event == "status" && m.Data.(map[string]any)["paused"] == true {
			paused = true
		}
	}
	if !paused {
		t.Fatal("viewers were not told about the pause")
	}
}

func TestEndStopsTheTransmission(t *testing.T) {
	rm := newRoom(t, newStore(t), translate.Fake{})
	audio := make(chan []byte)
	done := make(chan error, 1)
	go func() { done <- rm.Run(context.Background(), Transmission{}, audio) }()
	waitFor(t, func() bool { return rm.Status().Live })
	rm.End()
	select {
	case err := <-done:
		if !errors.Is(err, ErrEnded) {
			t.Fatalf("Run = %v, want ErrEnded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
	if st := rm.Status(); st.Live || !st.Ended {
		t.Fatalf("status %+v", st)
	}
	// The next transmission starts a new epoch and clears ended.
	audio = make(chan []byte)
	go feed(audio, 1)
	if err := rm.Run(context.Background(), Transmission{}, audio); err != nil {
		t.Fatal(err)
	}
	if st := rm.Status(); st.Ended || st.Epoch != 2 {
		t.Fatalf("status %+v", st)
	}
}

// The language spoken on stage is chosen per transmission: a room set up
// for English captions a Spanish talk as Spanish and translates it to
// English, instead of forcing the recognizer to English.
func TestRunTranscribesTheSpokenLanguage(t *testing.T) {
	st := newStore(t)
	rm := newRoom(t, st, translate.Fake{})
	sub, _, cancel := rm.Hub().Subscribe(64, "en", "")
	defer cancel()

	audio := make(chan []byte)
	go feed(audio, 1)
	if err := rm.Run(context.Background(), Transmission{Lang: "es"}, audio); err != nil {
		t.Fatal(err)
	}
	got := finals(sub)
	if len(got) != 1 || got[0].Lang != "es" || got[0].Text != "Bienvenidos a Nerdearla." ||
		got[0].Translations["en"] != "[en] Bienvenidos a Nerdearla." || len(got[0].Translations) != 1 {
		t.Fatalf("finals = %+v", got)
	}
	if s := rm.Status(); s.Source != "es" || len(s.Targets) != 1 || s.Targets[0] != "en" {
		t.Fatalf("status = %+v", s)
	}

	// Without a language the room keeps the last one.
	audio = make(chan []byte)
	go feed(audio, 1)
	if err := rm.Run(context.Background(), Transmission{}, audio); err != nil {
		t.Fatal(err)
	}
	if got := finals(sub); len(got) != 1 || got[0].Lang != "es" {
		t.Fatalf("second transmission = %+v", got)
	}

	if err := rm.Run(context.Background(), Transmission{Lang: "pt"}, make(chan []byte)); !errors.Is(err, ErrLanguage) {
		t.Fatalf("pt in an en/es room: %v", err)
	}
	if rm.Status().Live {
		t.Fatal("a rejected language left the room live")
	}
}

// run streams secs seconds of silence through one transmission.
func run(t *testing.T, rm *Room, tr Transmission, secs int) {
	t.Helper()
	audio := make(chan []byte)
	go feed(audio, secs)
	if err := rm.Run(context.Background(), tr, audio); err != nil {
		t.Fatal(err)
	}
}

func TestTalks(t *testing.T) {
	st := newStore(t)
	rm := newRoom(t, st, translate.Fake{})
	talkOf := func() (int, string) {
		t.Helper()
		recent := rm.Hub().Recent()
		last := recent[len(recent)-1]
		return last.Talk, last.TalkTitle
	}

	run(t, rm, Transmission{}, 1)
	if n, title := talkOf(); n != 1 || title != "" {
		t.Fatalf("first transmission: talk %d %q", n, title)
	}
	run(t, rm, Transmission{}, 1) // a reconnect continues the talk
	if n, _ := talkOf(); n != 1 {
		t.Fatalf("reconnect: talk %d, want 1", n)
	}
	run(t, rm, Transmission{Talk: "Keynote"}, 1)
	if n, title := talkOf(); n != 2 || title != "Keynote" {
		t.Fatalf("titled transmission: talk %d %q", n, title)
	}
	// Production opens the next talk during a long break: the operator's
	// next transmission goes into it.
	rm.idleSince = time.Now().Add(-2 * talkGap)
	if n := rm.NewTalk("Q&A"); n != 3 {
		t.Fatalf("NewTalk = %d, want 3", n)
	}
	run(t, rm, Transmission{}, 1)
	if n, title := talkOf(); n != 3 || title != "Q&A" {
		t.Fatalf("after NewTalk: talk %d %q", n, title)
	}

	// A restarted server continues the last talk, until a long break.
	again := newRoom(t, st, translate.Fake{})
	if s := again.Status(); s.Talk != 3 || s.TalkTitle != "Q&A" {
		t.Fatalf("after reload: %+v", s)
	}
	again.idleSince = time.Now().Add(-2 * talkGap)
	rm = again
	run(t, rm, Transmission{}, 1)
	if n, title := talkOf(); n != 4 || title != "" {
		t.Fatalf("after a break: talk %d %q", n, title)
	}
}

func TestStatusMetersAudio(t *testing.T) {
	rm := newRoom(t, newStore(t), translate.Fake{})
	audio := make(chan []byte)
	go func() {
		tone := make([]byte, asr.BytesPerSecond/10)
		for i := 0; i < len(tone); i += 4 { // ±16384: -6 dBFS
			tone[i+1], tone[i+3] = 0x40, 0xc0
		}
		for range 20 {
			audio <- tone
		}
		close(audio)
	}()
	start := time.Now()
	if err := rm.Run(context.Background(), Transmission{}, audio); err != nil {
		t.Fatal(err)
	}
	s := rm.Status()
	if s.AudioSeconds != 2 || s.LastAudioAt.Before(start) || s.LastTextAt.Before(start) {
		t.Fatalf("status = %+v", s)
	}
	if s.AudioLevel < -7 || s.AudioLevel > -5 {
		t.Fatalf("level = %.1f dBFS, want about -6", s.AudioLevel)
	}
}

// batcher translates batches, counting calls; with split set it loses the
// line breaks (the provider merged the lines).
type batcher struct {
	mu      sync.Mutex
	batches [][]string
	singles int
	split   bool
}

func (b *batcher) Translate(_ context.Context, text, _, dst string, _ translate.Glossary) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.singles++
	return "[" + dst + "] " + text, nil
}

func (b *batcher) TranslateBatch(_ context.Context, lines []string, _, dst string, _ translate.Glossary) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.batches = append(b.batches, lines)
	if b.split {
		return nil, translate.ErrBatchSplit
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "[" + dst + "] " + l
	}
	return out, nil
}

// TestRoomBatchesWaitingLines: an utterance that commits five sentences at
// once is translated in fewer requests than lines, every line in order.
func TestRoomBatchesWaitingLines(t *testing.T) {
	for _, split := range []bool{false, true} {
		var evs scripted
		for i := range 5 {
			evs = append(evs, asr.Event{Kind: asr.Final, Text: fmt.Sprintf("वाक्य %d।", i)})
		}
		b := &batcher{split: split}
		rm := newRoomWith(t, config.Room{ID: "a", Source: "hi", Targets: []string{"te"}}, newStore(t), evs, slowFirst{b})
		sub, _, cancel := rm.Hub().Subscribe(64, "te", "")
		audio := make(chan []byte)
		close(audio)
		if err := rm.Run(context.Background(), Transmission{}, audio); err != nil {
			t.Fatal(err)
		}
		got := finals(sub)
		cancel()
		for i, s := range got {
			if s.Translations["te"] != fmt.Sprintf("[te] वाक्य %d।", i) {
				t.Fatalf("split=%v line %d: %+v", split, i, s)
			}
		}
		if len(got) != 5 {
			t.Fatalf("split=%v: %d lines", split, len(got))
		}
		calls := len(b.batches) + b.singles
		if !split && calls >= 5 {
			t.Errorf("5 waiting lines took %d requests: %v", calls, b.batches)
		}
		if split && b.singles == 0 {
			t.Errorf("a batch that lost its line breaks was not retried line by line")
		}
	}
}

// slowFirst holds the first request a moment, so the next lines queue up
// behind it as they do behind a real network call.
type slowFirst struct{ *batcher }

func (s slowFirst) Translate(ctx context.Context, text, src, dst string, g translate.Glossary) (string, error) {
	s.mu.Lock()
	first := s.singles == 0 && len(s.batches) == 0
	s.mu.Unlock()
	if first {
		time.Sleep(50 * time.Millisecond)
	}
	return s.batcher.Translate(ctx, text, src, dst, g)
}

// recording keeps the glossary each translation was given, and the hints
// the recognizer was given.
type recording struct {
	mu       sync.Mutex
	glossary []translate.Glossary
	hints    []string
	lines    []string
	gate     chan struct{} // optional: hold each line until released
}

func (r *recording) Translate(_ context.Context, text, _, dst string, g translate.Glossary) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.glossary = append(r.glossary, g)
	return "[" + dst + "] " + text, nil
}

func (r *recording) Run(ctx context.Context, cfg asr.Config, audio <-chan []byte, events chan<- asr.Event) error {
	r.mu.Lock()
	r.hints = cfg.Vocabulary
	r.mu.Unlock()
	for _, l := range r.lines {
		if r.gate != nil {
			<-r.gate
		}
		events <- asr.Event{Kind: asr.Final, Text: l}
	}
	for range audio {
	}
	return nil
}

func version(n int, entries ...bank.Entry) bank.Version {
	for i := range entries {
		entries[i].Status = bank.Approved
	}
	v := bank.NewVersion(entries, "hall", "", "test", "")
	v.Number = n
	return v
}

// TestRoomUsesThePinnedVersion: hints and glossary come from the pinned
// Dictation Bank version, and a version pinned during a transmission
// applies from the next committed line.
func TestRoomUsesThePinnedVersion(t *testing.T) {
	rec := &recording{lines: []string{"राम कथा सुनो।", "फिर राम कथा।"}, gate: make(chan struct{})}
	cfg := config.Room{ID: "hall", Source: "hi", Targets: []string{"te"}, Glossary: []string{"Dubash"}}
	rm := newRoomWith(t, cfg, newStore(t), rec, rec)
	rm.SetVersion(version(1, bank.Entry{Scope: bank.ScopeEvent, ScopeID: "hall", Term: "राम कथा", Lang: "hi", Translations: map[string]string{"te": "రామకథ"}}))
	_, _, cancel := rm.Hub().Subscribe(64, "te", "")
	defer cancel()
	audio := make(chan []byte)
	done := make(chan error, 1)
	go func() { done <- rm.Run(context.Background(), Transmission{}, audio) }()
	rec.gate <- struct{}{} // first line under version 1
	waitFor(t, func() bool { rec.mu.Lock(); defer rec.mu.Unlock(); return len(rec.glossary) == 1 })
	rm.SetVersion(version(2, bank.Entry{Scope: bank.ScopeEvent, ScopeID: "hall", Term: "राम कथा", Lang: "hi", Translations: map[string]string{"te": "శ్రీరామ కథ"}}))
	rec.gate <- struct{}{} // second line under version 2
	close(audio)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rec.hints, []string{"राम कथा", "Dubash"}) {
		t.Errorf("recognizer hints %q", rec.hints)
	}
	if len(rec.glossary) != 2 || rec.glossary[0].Terms[0].Target != "రామకథ" || rec.glossary[1].Terms[0].Target != "శ్రీరామ కథ" {
		t.Fatalf("glossaries %+v", rec.glossary)
	}
	if !slices.Equal(rec.glossary[0].Keep, []string{"Dubash"}) {
		t.Errorf("rooms.yaml glossary not kept: %+v", rec.glossary[0])
	}
	if st := rm.Status(); st.ContextVersion != 2 || st.ContextEntries != 1 {
		t.Errorf("status %+v", st)
	}
}

func TestCorrectPublishesARevision(t *testing.T) {
	st := newStore(t)
	rm := newRoomWith(t, config.Room{ID: "hall", Source: "hi", Targets: []string{"te", "kn"}}, st,
		&recording{lines: []string{"डबस में स्वागत है।"}}, &recording{})
	sub, _, cancel := rm.Hub().Subscribe(64, "te", "")
	defer cancel()
	audio := make(chan []byte)
	close(audio)
	if err := rm.Run(context.Background(), Transmission{}, audio); err != nil {
		t.Fatal(err)
	}
	finals(sub) // drain
	before, after, err := rm.Correct(context.Background(), 0, "  Dubash  में स्वागत है। ")
	if err != nil {
		t.Fatal(err)
	}
	if before.Text != "डबस में स्वागत है।" || after.Text != "Dubash में स्वागत है।" || after.Raw != before.Text || after.Rev != 1 {
		t.Fatalf("before %+v after %+v", before, after)
	}
	var corr caption.Segment
	var tr caption.Translation
	deadline := time.After(2 * time.Second)
	for tr.Rev == 0 {
		select {
		case m := <-sub:
			switch d := m.Data.(type) {
			case caption.Segment:
				if m.Event == "correction" {
					corr = d
				}
			case caption.Translation:
				tr = d
			}
		case <-deadline:
			t.Fatal("no new translation")
		}
	}
	if corr.Rev != 1 || tr.Lang != "te" || tr.Text != "[te] Dubash में स्वागत है।" {
		t.Fatalf("correction %+v translation %+v (only the language being read)", corr, tr)
	}
	waitFor(t, func() bool {
		segs, _, _ := st.Load("hall")
		return len(segs) == 1 && segs[0].Rev == 1 && segs[0].Translations["te"] != ""
	})
	if r := rm.Hub().Recent(); r[0].Text != after.Text || r[0].Translations["te"] != tr.Text {
		t.Fatalf("recent %+v", r[0])
	}
	if _, _, err := rm.Correct(context.Background(), 99, "x"); !errors.Is(err, ErrUnknownSegment) {
		t.Fatalf("unknown line: %v", err)
	}
	if _, same, _ := rm.Correct(context.Background(), 0, "Dubash में स्वागत है।"); same.Rev != 1 {
		t.Fatalf("an identical text made a new revision: %+v", same)
	}
}

// TestBackfillTranslatesTheHistory: a reader who opens a language after
// lines were spoken gets them translated, once, in batches.
func TestBackfillTranslatesTheHistory(t *testing.T) {
	b := &batcher{}
	lines := []string{"एक।", "दो।", "तीन।", "चार।", "पाँच।"}
	rm := newRoomWith(t, config.Room{ID: "a", Source: "hi", Targets: []string{"te"}}, newStore(t), &recording{lines: lines}, b)
	audio := make(chan []byte)
	close(audio)
	if err := rm.Run(context.Background(), Transmission{}, audio); err != nil {
		t.Fatal(err)
	}
	if len(b.batches)+b.singles != 0 {
		t.Fatal("translated with no reader")
	}
	sub, _, cancel := rm.Hub().Subscribe(64, "te", "")
	defer cancel()
	rm.Backfill("te")
	got := map[int]string{}
	deadline := time.After(2 * time.Second)
	for len(got) < len(lines) {
		select {
		case m := <-sub:
			if tr, ok := m.Data.(caption.Translation); ok {
				got[tr.Segment] = tr.Text
			}
		case <-deadline:
			t.Fatalf("history translated: %v", got)
		}
	}
	if got[4] != "[te] पाँच।" || len(b.batches) != 1 {
		t.Fatalf("got %v in %d batches", got, len(b.batches))
	}
	rm.Backfill("te") // nothing left to translate
	rm.Backfill("hi") // the spoken language needs none
	time.Sleep(50 * time.Millisecond)
	if len(b.batches) != 1 || b.singles != 0 {
		t.Fatalf("translated again: %d batches, %d singles", len(b.batches), b.singles)
	}
	if r := rm.Hub().Recent(); r[0].Translations["te"] != "[te] एक।" {
		t.Fatalf("replay for later readers: %+v", r[0])
	}
}
