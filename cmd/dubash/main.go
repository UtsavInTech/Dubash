// Command dubash serves live captions and translations to phones.
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
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/genai"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/bank"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/correct"
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

	engine, translator, meter, err := backends(ctx, log, fake)
	if err != nil {
		return err
	}

	token := os.Getenv("ADMIN_TOKEN")
	if token == "" {
		log.Warn("ADMIN_TOKEN is not set: anyone can stream audio and see the admin panel")
	}

	corrMeter := &translate.Meter{}
	corrector, err := newCorrector(log, fake, corrMeter)
	if err != nil {
		return err
	}
	deps := room.Deps{ASR: engine, Translator: translator, Store: st, Log: log, Corrector: corrector}
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
	site.Meter, site.CorrectionMeter = meter, corrMeter
	if !fake {
		site.Prices = web.SarvamPrices
	}
	if site.Bank, err = openBank(ctx, log, dataDir); err != nil {
		return err
	}
	defer site.Bank.Close()
	// Each room starts with its pinned Dictation Bank version. A bank that
	// cannot be read leaves rooms with rooms.yaml's glossary only: the
	// control plane must not keep an event from running.
	for _, rm := range rs {
		v, ok, err := site.Bank.Pinned(ctx, rm.Config().ID)
		switch {
		case err != nil:
			log.Warn("could not load the pinned dictation bank version", "room", rm.Config().ID, "err", err)
		case ok:
			rm.SetVersion(v)
		}
	}
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

// newCorrector sets up the correction stage. CORRECTION is off, rules
// (the Dictation Bank's mis-hearings and near misses only) or model
// (rules, then Sarvam's chat model with the bank and the script, within
// CORRECTION_DEADLINE_MS, default 800). The default is model when
// SARVAM_API_KEY is set, rules otherwise.
func newCorrector(log *slog.Logger, fake bool, meter *translate.Meter) (*correct.Corrector, error) {
	mode := os.Getenv("CORRECTION")
	key := os.Getenv("SARVAM_API_KEY")
	if mode == "" {
		mode = "rules"
		if key != "" && !fake {
			mode = "model"
		}
	}
	deadline := 800 * time.Millisecond
	if v := os.Getenv("CORRECTION_DEADLINE_MS"); v != "" {
		ms, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("CORRECTION_DEADLINE_MS: %w", err)
		}
		deadline = time.Duration(ms) * time.Millisecond
	}
	switch mode {
	case "off":
		return nil, nil
	case "rules":
		return &correct.Corrector{}, nil
	case "model":
		if key == "" {
			return nil, errors.New("CORRECTION=model needs SARVAM_API_KEY")
		}
		chat := translate.SarvamChat{Key: key, Model: os.Getenv("SARVAM_CHAT_MODEL"), Meter: meter, Log: log}
		log.Info("correction stage: dictation bank rules and model", "deadline", deadline)
		return &correct.Corrector{Model: correct.LLM{Chat: chat}, Deadline: deadline}, nil
	}
	return nil, fmt.Errorf("unknown CORRECTION %q (off, rules, model)", mode)
}

// openBank opens the Dictation Bank: PostgreSQL at DATABASE_URL, or a JSON
// file in the data directory.
func openBank(ctx context.Context, log *slog.Logger, dataDir string) (bank.Store, error) {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		s, err := bank.OpenPG(ctx, url)
		if err != nil {
			return nil, err
		}
		log.Info("dictation bank: postgresql")
		return s, nil
	}
	path := filepath.Join(dataDir, "bank.json")
	log.Info("dictation bank: file (set DATABASE_URL for PostgreSQL)", "path", path)
	return bank.OpenFile(path)
}

// backends picks the transcription engine and the translator.
//
// ASR_PROVIDER and TRANSLATE_PROVIDER choose explicitly: sarvam, gemini,
// or whisper / chat (the OpenAI-compatible servers at ASR_URL and
// TRANSLATE_URL); TRANSLATE_PROVIDER=sarvam-translate uses Sarvam's
// translate API instead of its chat model. Without them: Sarvam when SARVAM_API_KEY is set (the
// default for Indian languages), else the URL servers when their URLs are
// set, else Gemini.
func backends(ctx context.Context, log *slog.Logger, fake bool) (asr.Engine, translate.Translator, *translate.Meter, error) {
	if fake {
		return asr.Fake{}, translate.Fake{}, nil, nil
	}
	var meter *translate.Meter
	var client *genai.Client
	gemini := func() (*genai.Client, error) {
		if client != nil {
			return client, nil
		}
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			return nil, errors.New("GEMINI_API_KEY is required for Gemini (or set SARVAM_API_KEY, or ASR_URL and TRANSLATE_URL, or run with -fake)")
		}
		c, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI})
		if err != nil {
			return nil, fmt.Errorf("gemini client: %w", err)
		}
		client = c
		return c, nil
	}
	sarvamKey := os.Getenv("SARVAM_API_KEY")
	pick := func(explicit, url string) string {
		switch {
		case explicit != "":
			return explicit
		case sarvamKey != "":
			return "sarvam"
		case url != "":
			return "url"
		}
		return "gemini"
	}

	var engine asr.Engine
	switch p := pick(os.Getenv("ASR_PROVIDER"), os.Getenv("ASR_URL")); p {
	case "sarvam":
		if sarvamKey == "" {
			return nil, nil, nil, errors.New("ASR_PROVIDER=sarvam needs SARVAM_API_KEY")
		}
		s := &asr.Sarvam{
			Key: sarvamKey, Model: os.Getenv("SARVAM_ASR_MODEL"), Log: log,
			Early: os.Getenv("SARVAM_EARLY") == "on",
		}
		if v := os.Getenv("SARVAM_SILENCE_MS"); v != "" {
			ms, err := strconv.Atoi(v)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("SARVAM_SILENCE_MS: %w", err)
			}
			s.Silence = time.Duration(ms) * time.Millisecond
		}
		engine = s
	case "url", "whisper":
		engine = &asr.Whisper{
			URL: os.Getenv("ASR_URL"), Model: os.Getenv("ASR_MODEL"), Key: os.Getenv("ASR_API_KEY"),
			Interim: 1500 * time.Millisecond,
		}
	case "gemini":
		c, err := gemini()
		if err != nil {
			return nil, nil, nil, err
		}
		engine = &asr.Gemini{Client: c, Model: os.Getenv("ASR_MODEL"), Log: log}
	default:
		return nil, nil, nil, fmt.Errorf("unknown ASR_PROVIDER %q (sarvam, gemini, whisper)", p)
	}

	var translator translate.Translator
	switch p := pick(os.Getenv("TRANSLATE_PROVIDER"), os.Getenv("TRANSLATE_URL")); p {
	case "sarvam", "sarvam-translate":
		if sarvamKey == "" {
			return nil, nil, nil, fmt.Errorf("TRANSLATE_PROVIDER=%s needs SARVAM_API_KEY", p)
		}
		rpm := 60 // Sarvam's Starter plan; Pro 200, Business 1000 (per account)
		if v := os.Getenv("SARVAM_TRANSLATE_RPM"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("SARVAM_TRANSLATE_RPM: %w", err)
			}
			rpm = n
		}
		if p == "sarvam" {
			// Sarvam's chat model: priced per token, about a fiftieth of the
			// translate API per caption hour, and faster.
			meter = &translate.Meter{}
			limiter := translate.NewRateLimiter(rpm)
			translator = translate.SarvamChat{
				Key: sarvamKey, Model: os.Getenv("SARVAM_CHAT_MODEL"), Limiter: limiter, Meter: meter, Log: log,
				// The few lines the model only transliterates (dialect it
				// does not understand) go to the translate API.
				Fallback: translate.Sarvam{Key: sarvamKey, Model: os.Getenv("SARVAM_TRANSLATE_MODEL"), Limiter: limiter},
			}
		} else {
			translator = translate.Sarvam{Key: sarvamKey, Model: os.Getenv("SARVAM_TRANSLATE_MODEL"), Limiter: translate.NewRateLimiter(rpm)}
		}
	case "url", "chat":
		translator = translate.Chat{URL: os.Getenv("TRANSLATE_URL"), Model: os.Getenv("TRANSLATE_MODEL"), Key: os.Getenv("TRANSLATE_API_KEY")}
	case "gemini":
		c, err := gemini()
		if err != nil {
			return nil, nil, nil, err
		}
		translator = translate.Hedged{Inner: translate.Gemini{Client: c, Model: os.Getenv("TRANSLATE_MODEL")}}
	default:
		return nil, nil, nil, fmt.Errorf("unknown TRANSLATE_PROVIDER %q (sarvam, gemini, chat)", p)
	}
	return engine, translator, meter, nil
}
