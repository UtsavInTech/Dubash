// Command dubash serves real-time conference captions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/genai"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/translate"
	"github.com/UtsavInTech/dubash/internal/web"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	rooms := flag.String("config", "rooms.yaml", "rooms file")
	data := flag.String("data", "data", "directory for transcripts")
	fake := flag.Bool("fake", false, "use scripted transcription and translation (no API key needed)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, *addr, *rooms, *data, *fake); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, addr, roomsPath, dataDir string, fake bool) error {
	cfg, err := config.Load(roomsPath)
	if err != nil {
		return err
	}
	st, err := store.New(dataDir)
	if err != nil {
		return err
	}

	engine, translator, err := backends(ctx, log, fake)
	if err != nil {
		return err
	}

	token := os.Getenv("ADMIN_TOKEN")
	if token == "" {
		log.Warn("ADMIN_TOKEN is not set: anyone can stream audio and see the admin panel")
	}

	deps := room.Deps{ASR: engine, Translator: translator, Store: st, Log: log}
	var rs []*room.Room
	for _, rc := range cfg.Rooms {
		rm, err := room.New(rc, deps)
		if err != nil {
			return err
		}
		rs = append(rs, rm)
	}

	site := web.New(rs, st, token, log)
	site.PublicURL = os.Getenv("PUBLIC_URL")
	srv := &http.Server{
		Addr:              addr,
		Handler:           site.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Cancel long-lived SSE and WebSocket requests on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("dubash listening", "addr", addr, "rooms", len(rs),
		"asr", fmt.Sprintf("%T", engine), "translator", fmt.Sprintf("%T", translator))

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// Docker gives a stopping container 10 s: stop accepting requests, then
	// let the rooms publish and store the lines still being translated.
	shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdown)
	if werr := site.Wait(shutdown); werr != nil {
		log.Warn("rooms still running at exit", "err", werr)
	}
	return err
}

// backends picks the transcription engine and the translator. Gemini is
// the default for both; ASR_URL and TRANSLATE_URL point either one at an
// OpenAI-compatible server instead (Whisper and Gemma on the venue's own
// hardware, for example), and GEMINI_API_KEY is only needed for what still
// uses Gemini.
func backends(ctx context.Context, log *slog.Logger, fake bool) (asr.Engine, translate.Translator, error) {
	if fake {
		return asr.Fake{}, translate.Fake{}, nil
	}
	var client *genai.Client
	gemini := func() (*genai.Client, error) {
		if client != nil {
			return client, nil
		}
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			return nil, errors.New("GEMINI_API_KEY is required unless ASR_URL and TRANSLATE_URL are set (or run with -fake)")
		}
		c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI})
		if err != nil {
			return nil, fmt.Errorf("gemini client: %w", err)
		}
		client = c
		return c, nil
	}

	var engine asr.Engine
	if u := os.Getenv("ASR_URL"); u != "" {
		engine = &asr.Whisper{
			URL: u, Model: os.Getenv("ASR_MODEL"), Key: os.Getenv("ASR_API_KEY"),
			Interim: 1500 * time.Millisecond,
		}
	} else {
		c, err := gemini()
		if err != nil {
			return nil, nil, err
		}
		engine = &asr.Gemini{Client: c, Model: os.Getenv("ASR_MODEL"), Log: log}
	}

	var translator translate.Translator
	if u := os.Getenv("TRANSLATE_URL"); u != "" {
		translator = translate.Chat{URL: u, Model: os.Getenv("TRANSLATE_MODEL"), Key: os.Getenv("TRANSLATE_API_KEY")}
	} else {
		c, err := gemini()
		if err != nil {
			return nil, nil, err
		}
		translator = translate.Hedged{Inner: translate.Gemini{Client: c, Model: os.Getenv("TRANSLATE_MODEL")}}
	}
	return engine, translator, nil
}
