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
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"google.golang.org/genai"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/bank"
	"github.com/UtsavInTech/dubash/internal/cluster"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/correct"
	"github.com/UtsavInTech/dubash/internal/eventdb"
	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/speaker"
	"github.com/UtsavInTech/dubash/internal/translate"
	"github.com/UtsavInTech/dubash/internal/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address (:8080 for phones on the network: needs a strong ADMIN_TOKEN)")
	rooms := flag.String("config", "rooms.yaml", "rooms file")
	data := flag.String("data", "data", "directory for transcripts")
	fake := flag.Bool("fake", false, "use scripted transcription and translation (no API key needed)")
	insecure := flag.Bool("insecure", false, "start on a public address with an empty or weak ADMIN_TOKEN (never for an event)")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, *addr, *rooms, *data, *fake, *insecure); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, addr, roomsPath, dataDir string, fake, insecure bool) error {
	var cfg config.Config
	var err error
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL != "" {
		db, openErr := eventdb.Open(ctx, databaseURL)
		if openErr != nil {
			return openErr
		}
		cfg, err = eventdb.Catalog(ctx, db, roomsPath)
		db.Close()
	} else {
		cfg, err = config.Load(roomsPath)
		if err == nil {
			cfg, err = config.WithCreated(cfg, filepath.Join(dataDir, "events.yaml"))
		}
	}
	if err != nil {
		return err
	}
	catalog := cfg.Rooms // every event, for QR join codes
	var coordinator *cluster.Coordinator
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		if databaseURL == "" {
			return errors.New("REDIS_URL requires DATABASE_URL for shared events, transcripts and vocabulary")
		}
		coordinator, ctx, err = cluster.Start(ctx, redisURL, os.Getenv("NODE_URL"), os.Getenv("CLUSTER_NAMESPACE"), os.Getenv("ROOM_IDS"), cfg)
		if err != nil {
			return err
		}
		defer coordinator.Close()
		cfg.Rooms = coordinator.Owned // this server runs only its shard
	} else if os.Getenv("ROOM_IDS") != "" || os.Getenv("NODE_URL") != "" {
		return errors.New("ROOM_IDS and NODE_URL require REDIS_URL")
	}
	var st *store.Store
	if databaseURL != "" {
		st, err = store.OpenPG(ctx, databaseURL)
	} else {
		st, err = store.New(dataDir)
	}
	if err != nil {
		return err
	}
	defer st.Close()

	engine, translator, meter, err := backends(ctx, log, fake)
	if err != nil {
		return err
	}

	token := os.Getenv("ADMIN_TOKEN")
	if err := checkToken(token, os.Getenv("TOKEN_SECRET"), os.Getenv("PUBLIC_URL"), addr); err != nil {
		if !insecure {
			return err
		}
		log.Warn("starting anyway (-insecure)", "problem", err)
	} else if token == "" || weakToken(token) {
		log.Warn("ADMIN_TOKEN is empty or weak: fine on this computer, never on a public address")
	}
	for _, rc := range cfg.Rooms {
		if !rc.Stores() {
			st.Disable(rc.ID)
		}
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
	site.Demo = fake
	// Noise removal for the speaker console (DeepFilterNet3): from a CDN
	// (DENOISE_URL), else served from DENOISE_DIR.
	site.DenoiseURL = os.Getenv("DENOISE_URL")
	site.DenoiseDir = os.Getenv("DENOISE_DIR")
	if site.DenoiseDir == "" {
		site.DenoiseDir = "third_party/deepfilternet"
	}
	site.TemporaryData = os.Getenv("DUBASH_TEMPORARY_DATA") == "on"
	site.Catalog = catalog
	if coordinator == nil {
		site.CreateEvent = func(createCtx context.Context, rc config.Room) (*room.Room, error) {
			rm, err := room.New(rc, deps)
			if err != nil {
				return nil, err
			}
			if databaseURL != "" {
				db, err := eventdb.Open(createCtx, databaseURL)
				if err != nil {
					return nil, err
				}
				defer db.Close()
				if err = eventdb.Add(createCtx, db, rc); err != nil {
					return nil, err
				}
			} else if err = config.AddCreated(filepath.Join(dataDir, "events.yaml"), rc); err != nil {
				return nil, err
			}
			return rm, nil
		}
	}
	site.UseState(dataDir, os.Getenv("TOKEN_SECRET"))
	site.TrustProxy = os.Getenv("TRUST_PROXY") == "on"
	// Sign in with Google for the listed organisers (comma separated).
	site.UseGoogle(os.Getenv("GOOGLE_CLIENT_ID"), strings.Split(os.Getenv("ORGANISER_EMAILS"), ","))
	if v, err := strconv.Atoi(os.Getenv("MAX_VIEWERS")); err == nil {
		site.MaxViewers = v
	}
	if v, err := strconv.Atoi(os.Getenv("MAX_VIEWERS_PER_IP")); err == nil {
		site.MaxViewersPerIP = v
	}
	site.Meter, site.CorrectionMeter = meter, corrMeter
	if !fake {
		site.Prices = web.SarvamPrices
	}
	if site.Bank, err = openBank(ctx, log, dataDir); err != nil {
		return err
	}
	defer site.Bank.Close()
	if databaseURL != "" {
		site.Speakers, err = speaker.OpenPG(ctx, databaseURL)
	} else {
		site.Speakers, err = speaker.OpenFile(filepath.Join(dataDir, "speakers.json"))
	}
	if err != nil { return err }
	defer site.Speakers.Close()
	site.SpeakerDir = filepath.Join(dataDir, "speakers")
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
	go site.Retain(ctx)
	local := site.Handler()
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			readyCtx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			if err := st.Ready(readyCtx); err != nil {
				http.Error(w, "transcript database unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte("ok"))
			return
		}
		local.ServeHTTP(w, r)
	})
	if coordinator != nil {
		handler = coordinator.Handler(handler)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
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
	if fake {
		mode = "rules"
	}
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
	// LEXICON lists word files (JSON lines with term and language, such as
	// the Indian discourse vocabulary bank's vocabulary.jsonl): real words
	// the near-miss rule must not "correct" into bank terms.
	var lex correct.Lexicon
	if v := os.Getenv("LEXICON"); v != "" && mode != "off" {
		var err error
		if lex, err = correct.LoadLexicon(strings.Split(v, ",")...); err != nil {
			return nil, err
		}
		n := 0
		for _, ws := range lex {
			n += len(ws)
		}
		log.Info("lexicon loaded", "words", n, "languages", len(lex))
	}
	switch mode {
	case "off":
		return nil, nil
	case "rules":
		return &correct.Corrector{Lexicon: lex}, nil
	case "model":
		if key == "" {
			return nil, errors.New("CORRECTION=model needs SARVAM_API_KEY")
		}
		chat := translate.SarvamChat{Key: key, Model: os.Getenv("SARVAM_CHAT_MODEL"), Meter: meter, Log: log}
		log.Info("correction stage: dictation bank rules and model", "deadline", deadline)
		return &correct.Corrector{Model: correct.LLM{Chat: chat}, Deadline: deadline, Lexicon: lex}, nil
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
			// Lines appear at sentence ends; when the speaker runs on (or
			// noise hides the pauses) for SARVAM_EARLY_AFTER_MS, default 4 s,
			// the stable words are committed at the nearest phrase end.
			// SARVAM_EARLY=off waits for sentence ends only.
			Early: os.Getenv("SARVAM_EARLY") != "off",
		}
		if v := os.Getenv("SARVAM_EARLY_AFTER_MS"); v != "" {
			ms, err := strconv.Atoi(v)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("SARVAM_EARLY_AFTER_MS: %w", err)
			}
			s.EarlyAfter = time.Duration(ms) * time.Millisecond
		} else {
			s.EarlyAfter = 4 * time.Second
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
	if !fake {
		if u := os.Getenv("ASR_BACKUP_URL"); u != "" {
			backup := &asr.Whisper{URL: u, Model: os.Getenv("ASR_BACKUP_MODEL"), Key: os.Getenv("ASR_BACKUP_API_KEY"), Interim: 1500 * time.Millisecond}
			engine = &asr.Failover{Primary: engine, Backup: backup}
		}
		if u := os.Getenv("TRANSLATE_BACKUP_URL"); u != "" {
			backup := translate.Chat{URL: u, Model: os.Getenv("TRANSLATE_BACKUP_MODEL"), Key: os.Getenv("TRANSLATE_BACKUP_API_KEY")}
			translator = &translate.Failover{Primary: translator, Backup: backup}
		}
	}
	return engine, translator, meter, nil
}

// minToken is the shortest ADMIN_TOKEN (and TOKEN_SECRET) accepted on a
// public address: `openssl rand -base64 24` gives 32 characters.
const minToken = 20

func weakToken(t string) bool {
	return len(t) < minToken || slices.Contains([]string{"change-me", "un-secreto", "secreto", "secret", "admin", "password"}, t)
}

// checkToken refuses an empty or weak admin token on a server others can
// reach: one listening beyond this computer, or with a public URL.
func checkToken(token, secret, publicURL, addr string) error {
	if !public(publicURL, addr) {
		return nil
	}
	switch {
	case token == "":
		return errors.New("ADMIN_TOKEN is not set: anyone could stream audio and run the admin panel (set one, e.g. `openssl rand -base64 24`, or listen on this computer only: -addr 127.0.0.1:8080)")
	case weakToken(token):
		return fmt.Errorf("ADMIN_TOKEN is too weak: use at least %d random characters, e.g. `openssl rand -base64 24`", minToken)
	case secret != "" && len(secret) < minToken:
		return fmt.Errorf("TOKEN_SECRET must be at least %d random characters", minToken)
	}
	return nil
}

// public reports whether the server can be reached from other machines.
func public(publicURL, addr string) bool {
	if u, err := url.Parse(publicURL); err == nil && u.Hostname() != "" && !loopback(u.Hostname()) {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return true
	}
	return !loopback(host)
}

func loopback(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
