package room

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/caption"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/translate"
)

type failTranslator struct{}

func (failTranslator) Translate(context.Context, string, string, string, []string) (string, error) {
	return "", errors.New("boom")
}

func newRoom(t *testing.T, st *store.Store, tr translate.Translator) *Room {
	t.Helper()
	rm, err := New(
		config.Room{ID: "sala-a", Title: "A", Source: "en", Targets: []string{"es"}},
		Deps{ASR: asr.Fake{Every: 1}, Translator: tr, Store: st, Log: slog.New(slog.DiscardHandler)},
	)
	if err != nil {
		t.Fatal(err)
	}
	return rm
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

func finals(sub <-chan Msg) []caption.Segment {
	var out []caption.Segment
	for len(sub) > 0 {
		if m := <-sub; m.Event == "final" {
			out = append(out, m.Data.(caption.Segment))
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
	sub, _, cancel := rm.Hub().Subscribe(64)
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
	if first.ID != 0 || first.Room != "sala-a" || first.Lang != "en" || first.T0 != 0 || first.T1 != 1 ||
		first.Text != "Welcome to Nerdearla." || first.Translations["es"] != "[es] Welcome to Nerdearla." {
		t.Fatalf("first = %+v", first)
	}
	if got[1].ID != 1 || got[1].T0 != 1 || got[1].T1 != 2 {
		t.Fatalf("second = %+v", got[1])
	}
	stored, _, _ := st.Load("sala-a")
	if len(stored) != 2 {
		t.Fatalf("stored = %d, want 2", len(stored))
	}
	s := rm.Status()
	if s.Live || s.Segments != 2 || s.Errors != 0 || s.LastSegmentAt.IsZero() {
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
	rm := newRoom(t, newStore(t), failTranslator{})
	sub, _, cancel := rm.Hub().Subscribe(64)
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
	sub, _, cancel := again.Hub().Subscribe(64)
	defer cancel()
	audio = make(chan []byte)
	go feed(audio, 1)
	_ = again.Run(context.Background(), Transmission{}, audio)
	got := finals(sub)
	if len(got) != 1 || got[0].ID != 2 || got[0].T0 != 2 || got[0].T1 != 3 {
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

func (g gated) Translate(ctx context.Context, text, _, dst string, _ []string) (string, error) {
	<-g
	return "[" + dst + "] " + text, nil
}

func TestRoomKeepsCommittedTextOnTheLiveLineUntilPublished(t *testing.T) {
	release := make(gated)
	rm, err := New(
		config.Room{ID: "sala-a", Title: "A", Source: "en", Targets: []string{"es"}},
		Deps{ASR: scripted{
			{Kind: asr.Interim, Text: "Welcome to Nerdearla. Today"},
			{Kind: asr.Final, Text: "Welcome to Nerdearla."},
			{Kind: asr.Interim, Text: "Today, we"},
		}, Translator: release, Store: newStore(t), Log: slog.New(slog.DiscardHandler)},
	)
	if err != nil {
		t.Fatal(err)
	}
	sub, _, cancel := rm.Hub().Subscribe(64)
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
	text := func(m Msg) string { return m.Data.(map[string]string)["text"] }

	if m := next(); m.Event != "interim" || text(m) != "Welcome to Nerdearla. Today" {
		t.Fatalf("got %+v", m)
	}
	// While the sentence is being translated it stays on the live line.
	if m := next(); m.Event != "interim" || text(m) != "Welcome to Nerdearla. Today, we" {
		t.Fatalf("got %+v", m)
	}
	release <- struct{}{}
	if m := next(); m.Event != "final" || m.Data.(caption.Segment).Text != "Welcome to Nerdearla." {
		t.Fatalf("got %+v", m)
	}
	// Right after the final, the live line comes back without it.
	if m := next(); m.Event != "interim" || text(m) != "Today, we" {
		t.Fatalf("got %+v", m)
	}
	close(audio)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// The language spoken on stage is chosen per transmission: a room set up
// for English captions a Spanish talk as Spanish and translates it to
// English, instead of forcing the recognizer to English.
func TestRunTranscribesTheSpokenLanguage(t *testing.T) {
	st := newStore(t)
	rm := newRoom(t, st, translate.Fake{})
	sub, _, cancel := rm.Hub().Subscribe(64)
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
