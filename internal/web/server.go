// Package web serves the audience, operator and admin pages and the
// streaming endpoints behind them.
package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"rsc.io/qr"

	"github.com/UtsavInTech/dubash/internal/caption"
	"github.com/UtsavInTech/dubash/internal/export"
	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/store"
)

//go:embed static
var embedded embed.FS

// Server routes requests to rooms.
type Server struct {
	rooms  map[string]*room.Room
	order  []*room.Room
	store  *store.Store
	token  string
	log    *slog.Logger
	static fs.FS

	// PublicURL is the audience-facing base URL (https://subs.example.org)
	// encoded in QR codes. Empty means the URL the request came in on.
	PublicURL string

	// drainGrace is how long a room's pipeline may keep finishing after
	// its operator disconnects before it is cancelled.
	drainGrace time.Duration

	// sessions counts audio sessions whose pipeline is still running.
	// http.Server.Shutdown does not wait for them: they are hijacked.
	sessions sync.WaitGroup
}

// New serves rooms in the given order. An empty token disables auth.
func New(rooms []*room.Room, st *store.Store, token string, log *slog.Logger) *Server {
	static, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err) // embedded tree is fixed at build time
	}
	s := &Server{rooms: map[string]*room.Room{}, order: rooms, store: st, token: token, log: log, static: static}
	for _, rm := range rooms {
		s.rooms[rm.Config().ID] = rm
	}
	return s
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(s.static)))
	mux.HandleFunc("GET /{$}", s.page("index.html"))
	mux.HandleFunc("GET /r/{room}", s.page("viewer.html"))
	mux.HandleFunc("GET /operator/{room}", s.page("operator.html"))
	mux.HandleFunc("GET /admin", s.page("admin.html"))
	mux.HandleFunc("GET /posters", s.page("posters.html"))
	mux.HandleFunc("GET /sw.js", s.serviceWorker)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") })
	mux.HandleFunc("GET /api/rooms", s.listRooms)
	mux.HandleFunc("GET /api/rooms/{room}/talks", s.listTalks)
	mux.HandleFunc("GET /api/site", s.site)
	mux.HandleFunc("GET /qr/{room}", s.qr)
	mux.HandleFunc("GET /api/admin/status", s.auth(s.adminStatus))
	mux.HandleFunc("POST /api/admin/rooms/{room}/talks", s.auth(s.newTalk))
	mux.HandleFunc("POST /api/admin/rooms/{room}/pause", s.auth(s.control(func(rm *room.Room) { rm.Pause() })))
	mux.HandleFunc("POST /api/admin/rooms/{room}/resume", s.auth(s.control(func(rm *room.Room) { rm.Resume() })))
	mux.HandleFunc("POST /api/admin/rooms/{room}/end", s.auth(s.control(func(rm *room.Room) { rm.End() })))
	mux.HandleFunc("GET /metrics", s.auth(s.metrics))
	mux.HandleFunc("GET /events/{room}", s.events)
	mux.HandleFunc("GET /ingest/{room}", s.auth(s.ingest))
	mux.HandleFunc("GET /export/{room}/{format}", s.export)
	mux.Handle("GET /samples/", http.StripPrefix("/samples/", http.FileServer(http.Dir("samples"))))
	return mux
}

// page serves one of the embedded HTML pages.
func (s *Server) page(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(s.static, name)
		if err != nil {
			http.Error(w, "page missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	}
}

// serviceWorker serves sw.js from the root, the scope it needs to answer
// for /r/ pages, uncached so a new version reaches OBS on its next load.
func (s *Server) serviceWorker(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.static, "sw.js")
	if err != nil {
		http.Error(w, "service worker missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

// auth requires the admin token, as a Bearer header or a token query
// parameter (browsers cannot set headers on WebSocket requests).
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" {
			next(w, r)
			return
		}
		got := r.URL.Query().Get("token")
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			got = strings.TrimPrefix(h, "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) room(w http.ResponseWriter, r *http.Request) (*room.Room, bool) {
	rm, ok := s.rooms[r.PathValue("room")]
	if !ok {
		http.Error(w, "unknown room", http.StatusNotFound)
	}
	return rm, ok
}

type roomInfo struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Speaker   string   `json:"speaker,omitempty"`
	Source    string   `json:"source"`
	Targets   []string `json:"targets"`
	Live      bool     `json:"live"`
	Paused    bool     `json:"paused,omitempty"`
	Ended     bool     `json:"ended,omitempty"`
	TalkTitle string   `json:"talkTitle,omitempty"`
}

func (s *Server) listRooms(w http.ResponseWriter, _ *http.Request) {
	out := make([]roomInfo, 0, len(s.order))
	for _, rm := range s.order {
		st := rm.Status()
		out = append(out, roomInfo{ID: st.ID, Title: st.Title, Speaker: st.Speaker, Source: st.Source, Targets: st.Targets,
			Live: st.Live, Paused: st.Paused, Ended: st.Ended, TalkTitle: st.TalkTitle})
	}
	writeJSON(w, out)
}

func (s *Server) adminStatus(w http.ResponseWriter, _ *http.Request) {
	out := make([]room.Status, 0, len(s.order))
	for _, rm := range s.order {
		out = append(out, rm.Status())
	}
	writeJSON(w, out)
}

// baseURL is where the audience reaches this server: PublicURL, or the
// scheme and host of r (honouring a TLS-terminating proxy).
func (s *Server) baseURL(r *http.Request) string {
	if s.PublicURL != "" {
		return strings.TrimRight(s.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) site(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"url": s.baseURL(r)})
}

// qr serves an SVG QR code that opens the room's captions, for the stage
// screen and printed posters. An optional lang pins the language;
// without it the viewer picks the reader's browser language.
func (s *Server) qr(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	cfg := rm.Config()
	target := s.baseURL(r) + "/r/" + cfg.ID
	if lang := r.URL.Query().Get("lang"); slices.Contains(cfg.Languages(), lang) {
		target += "?lang=" + lang
	}
	code, err := qr.Encode(target, qr.M)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	io.WriteString(w, qrSVG(code))
}

// qrSVG draws code as one path, one horizontal run of dark modules per
// subpath, inside the 4-module quiet zone scanners need.
func qrSVG(code *qr.Code) string {
	const quiet = 4
	n := code.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, n, n)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n)
	for y := range code.Size {
		for x := 0; x < code.Size; {
			if !code.Black(x, y) {
				x++
				continue
			}
			run := 0
			for x+run < code.Size && code.Black(x+run, y) {
				run++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dz", x+quiet, y+quiet, run, run)
			x += run
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// events streams a room's captions with Server-Sent Events. The lang
// parameter is the language the viewer reads: it receives the committed
// source lines, their translations into lang (which starts translating
// lang if nobody else reads it), and the live line. Committed messages
// carry SSE ids, so a reconnecting browser (which sends Last-Event-ID on
// its own) receives exactly what it missed.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	lang := r.URL.Query().Get("lang")
	if !slices.Contains(rm.Config().Languages(), lang) {
		lang = ""
	}
	lastID := r.Header.Get("Last-Event-ID")
	if lastID == "" {
		lastID = r.URL.Query().Get("after")
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")

	hub := rm.Hub()
	msgs, replay, cancel := hub.Subscribe(64, lang, lastID)
	defer cancel()
	writeEvent(w, "status", rm.StatusEvent())
	for _, m := range replay {
		writeMsg(w, m, hub.Instance())
	}
	// Marks the end of the replay, so an overlay can show only new lines.
	writeEvent(w, "caught-up", struct{}{})
	flusher.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m, ok := <-msgs:
			if !ok {
				return // dropped for falling behind; the browser reconnects
			}
			if writeMsg(w, m, hub.Instance()) != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeMsg writes a hub message, with its id when it has one.
func writeMsg(w io.Writer, m room.Msg, instance string) error {
	if id := m.ID(instance); id != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", id); err != nil {
			return err
		}
	}
	return writeEvent(w, m.Event, m.Data)
}

// control runs an action on a room from the panel or the operator console
// and answers with the room's status.
func (s *Server) control(action func(*room.Room)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rm, ok := s.room(w, r)
		if !ok {
			return
		}
		action(rm)
		s.log.Info("room control", "room", rm.Config().ID, "action", r.URL.Path)
		writeJSON(w, rm.Status())
	}
}

func writeEvent(w io.Writer, event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	return err
}

// StatusEventEnded closes an operator's socket when the event was ended
// from the panel: clients must not reconnect on their own.
const StatusEventEnded websocket.StatusCode = 4001

// defaultDrainGrace covers the ASR engine's own drain after the audio
// ends; pipelines still running after it are cancelled.
const defaultDrainGrace = 15 * time.Second

// maxCloseReason is the most a WebSocket close reason may carry (a control
// frame holds 125 bytes, two of them for the status code).
const maxCloseReason = 123

// maxTalkTitle bounds a talk title, in bytes.
const maxTalkTitle = 200

// Wait blocks until every audio session has ended and its last captions
// are stored, or until ctx is done. Call it after http.Server.Shutdown,
// which does not wait for WebSocket connections.
func (s *Server) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.sessions.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ingest receives an operator's PCM audio over a WebSocket. The optional
// lang query parameter is the language spoken on stage; without it the room
// keeps the language of its last transmission. The optional talk parameter
// titles the talk (see room.Transmission).
//
// The socket is read by its own goroutine that never blocks on the
// pipeline: when the audio buffer is full, chunks are dropped. That keeps
// the operator's disconnect observable even if the pipeline stops
// consuming, so a stuck engine can never keep the room live forever.
func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	lang := r.URL.Query().Get("lang")
	if lang != "" && !slices.Contains(rm.Config().Languages(), lang) {
		http.Error(w, room.ErrLanguage.Error(), http.StatusBadRequest)
		return
	}
	talk := strings.TrimSpace(r.URL.Query().Get("talk"))
	if len(talk) > maxTalkTitle {
		talk = strings.ToValidUTF8(talk[:maxTalkTitle], "")
	}
	if rm.Status().Live {
		http.Error(w, room.ErrBusy.Error(), http.StatusConflict)
		return
	}
	// Counted before the connection is hijacked, so that once Shutdown
	// has returned no session can start behind Wait's back.
	s.sessions.Add(1)
	defer s.sessions.Done()
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote the HTTP error
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	// A hijacked connection's request context is not cancelled when the
	// client goes away, so the handler cancels the pipeline itself.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	id := rm.Config().ID
	s.log.Info("operator connected", "room", id)

	audio := make(chan []byte, 50)
	done := make(chan error, 1)
	go func() { done <- rm.Run(ctx, room.Transmission{Lang: lang, Talk: talk}, audio) }()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer close(audio)
		dropped := 0
		defer func() {
			if dropped > 0 {
				s.log.Warn("dropped operator audio: pipeline not keeping up", "room", id, "chunks", dropped)
			}
		}()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageBinary {
				continue
			}
			select {
			case audio <- data:
			default:
				dropped++
			}
		}
	}()

	var runErr error
	select {
	case runErr = <-done: // the pipeline ended on its own (busy, fatal error)
	case <-readDone: // the operator left: let the pipeline finish, briefly
		grace := s.drainGrace
		if grace <= 0 {
			grace = defaultDrainGrace
		}
		t := time.NewTimer(grace)
		select {
		case runErr = <-done:
		case <-t.C:
			s.log.Warn("pipeline did not finish after the operator left; cancelling", "room", id)
			cancel()
			runErr = <-done
		}
		t.Stop()
	}
	s.log.Info("operator disconnected", "room", id)
	switch {
	case errors.Is(runErr, room.ErrBusy):
		c.Close(websocket.StatusPolicyViolation, "room busy")
	case errors.Is(runErr, room.ErrEnded):
		c.Close(StatusEventEnded, "event ended")
	case runErr != nil && !errors.Is(runErr, context.Canceled):
		s.log.Error("room session failed", "room", id, "err", runErr)
		c.Close(websocket.StatusInternalError, closeReason(runErr))
	default:
		c.Close(websocket.StatusNormalClosure, "")
	}
	c.CloseNow()
	cancel()
	<-readDone
}

// closeReason tells the operator why transcription failed: the innermost
// cause of err, without URLs (they are long and may carry credentials),
// trimmed to fit a close frame without splitting a rune.
func closeReason(err error) string {
	for u := errors.Unwrap(err); u != nil; u = errors.Unwrap(err) {
		err = u
	}
	words := strings.Fields(err.Error())
	words = slices.DeleteFunc(words, func(w string) bool { return strings.Contains(w, "://") })
	msg := "transcripción: " + strings.Join(words, " ")
	if len(msg) <= maxCloseReason {
		return msg
	}
	msg = msg[:maxCloseReason]
	for !utf8.ValidString(msg) {
		msg = msg[:len(msg)-1]
	}
	return msg
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	cfg := rm.Config()
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = cfg.Source
	}
	if !slices.Contains(cfg.Languages(), lang) {
		http.Error(w, "unknown language for this room", http.StatusBadRequest)
		return
	}
	render, contentType := map[string]func([]caption.Segment, string) string{
		"vtt": export.VTT, "srt": export.SRT, "txt": export.TXT,
	}, map[string]string{
		"vtt": "text/vtt; charset=utf-8", "srt": "application/x-subrip; charset=utf-8", "txt": "text/plain; charset=utf-8",
	}
	format := r.PathValue("format")
	fn, ok := render[format]
	if !ok {
		http.Error(w, "unknown format", http.StatusNotFound)
		return
	}
	segs, _, err := s.store.Load(cfg.ID)
	if err != nil {
		http.Error(w, "cannot read transcript", http.StatusInternalServerError)
		return
	}
	name := cfg.ID
	if v := r.URL.Query().Get("talk"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			http.Error(w, "talk must be a number", http.StatusBadRequest)
			return
		}
		// A talk that has begun but has no lines yet exports empty.
		if segs = export.Select(segs, n); segs == nil && (n < 1 || n > rm.Status().Talk) {
			http.Error(w, "no such talk", http.StatusNotFound)
			return
		}
		name = fmt.Sprintf("%s-talk%d", cfg.ID, n)
	}
	w.Header().Set("Content-Type", contentType[format])
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.%s"`, name, lang, format))
	io.WriteString(w, fn(segs, lang))
}

// listTalks lists the talks of a room's transcript, oldest first.
func (s *Server) listTalks(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	segs, _, err := s.store.Load(rm.Config().ID)
	if err != nil {
		http.Error(w, "cannot read transcript", http.StatusInternalServerError)
		return
	}
	talks := export.Talks(segs)
	if talks == nil {
		talks = []export.Talk{}
	}
	writeJSON(w, talks)
}

// newTalk starts a new talk in a room, titled by the title form value,
// so a room fed without interruption (dubash-ingest) is exported talk
// by talk.
func (s *Server) newTalk(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if len(title) > maxTalkTitle {
		http.Error(w, "title too long", http.StatusBadRequest)
		return
	}
	n := rm.NewTalk(title)
	s.log.Info("new talk", "room", rm.Config().ID, "talk", n, "title", title)
	writeJSON(w, map[string]any{"talk": n, "title": title})
}
