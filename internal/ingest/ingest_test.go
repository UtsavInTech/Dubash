package ingest

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/translate"
	"github.com/UtsavInTech/dubash/internal/web"
)

func newServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	rm, err := room.New(config.Room{ID: "sala-a", Title: "A", Source: "en", Targets: []string{"es"}},
		room.Deps{ASR: asr.Fake{Every: 1}, Translator: translate.Fake{}, Store: st, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(web.New([]*room.Room{rm}, st, "secret", log).Handler())
	t.Cleanup(srv.Close)
	return srv, st
}

func TestStreamCaptionsAFile(t *testing.T) {
	srv, st := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	audio := bytes.NewReader(make([]byte, 3*asr.BytesPerSecond+ChunkBytes/2))
	if err := Stream(ctx, Options{Server: srv.URL, Room: "sala-a", Token: "secret"}, audio); err != nil {
		t.Fatal(err)
	}
	// The server finishes the pipeline after the socket closes.
	for {
		segs, _, err := st.Load("sala-a")
		if err != nil {
			t.Fatal(err)
		}
		if len(segs) == 3 {
			if segs[0].Translations["es"] != "[es] Welcome to Nerdearla." {
				t.Fatalf("segment = %+v", segs[0])
			}
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("got %d segments, want 3", len(segs))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStreamStopsOnBadTokenOrRoom(t *testing.T) {
	srv, _ := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, tc := range []struct {
		opt  Options
		want error
	}{
		{Options{Server: srv.URL, Room: "sala-a", Token: "wrong"}, ErrUnauthorized},
		{Options{Server: srv.URL, Room: "nope", Token: "secret"}, ErrUnknownRoom},
		{Options{Server: srv.URL, Room: "sala-a", Token: "secret", Lang: "pt"}, ErrLanguage},
	} {
		err := Stream(ctx, tc.opt, bytes.NewReader(make([]byte, ChunkBytes)))
		if !errors.Is(err, tc.want) {
			t.Errorf("%+v: err = %v, want %v", tc.opt, err, tc.want)
		}
	}
}

func TestStreamWaitsForABusyRoom(t *testing.T) {
	srv, st := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	operator, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(200*time.Millisecond, func() { operator.Close(websocket.StatusNormalClosure, "") })

	opt := Options{Server: srv.URL, Room: "sala-a", Token: "secret", MinBackoff: 20 * time.Millisecond}
	if err := Stream(ctx, opt, bytes.NewReader(make([]byte, 2*asr.BytesPerSecond))); err != nil {
		t.Fatal(err)
	}
	for {
		segs, _, _ := st.Load("sala-a")
		if len(segs) == 2 {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("got %d segments, want 2", len(segs))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStreamExplainsARefusedConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := session(ctx, "ws://127.0.0.1:1/ingest/sala-a", make(chan []byte), slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "is the Dubash server running?") {
		t.Fatalf("err = %v", err)
	}
}

func TestEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:8080":     "ws://localhost:8080/ingest/sala-a?token=t%26k",
		"https://subs.example.org/": "wss://subs.example.org/ingest/sala-a?token=t%26k",
		"https://example.org/subs":  "wss://example.org/subs/ingest/sala-a?token=t%26k",
	} {
		got, err := Options{Server: in, Room: "sala-a", Token: "t&k"}.Endpoint()
		if err != nil || got != want {
			t.Errorf("%s → %q, %v; want %q", in, got, err, want)
		}
	}
	got, err := Options{Server: "http://h", Room: "a", Lang: "es", Talk: "Q&A"}.Endpoint()
	if want := "ws://h/ingest/a?lang=es&talk=Q%26A"; err != nil || got != want {
		t.Errorf("with lang and talk: %q, %v; want %q", got, err, want)
	}
	if _, err := (Options{Server: "ftp://x", Room: "a"}).Endpoint(); err == nil {
		t.Error("ftp accepted")
	}
}

func TestPushDropsOldestWhenFull(t *testing.T) {
	out := make(chan []byte, 2)
	for i := range 4 {
		push(out, []byte{byte(i)})
	}
	if a, b := <-out, <-out; a[0] != 2 || b[0] != 3 {
		t.Fatalf("kept %v %v, want the newest", a, b)
	}
}
