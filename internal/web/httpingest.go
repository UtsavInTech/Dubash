// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/UtsavInTech/dubash/internal/room"
)

// Audio over plain HTTP, for hosts whose gateway drops WebSockets (Zoho
// Catalyst AppSail does). The console starts a session, then posts about
// a quarter second of PCM per request, in order, and ends it; the room's
// pipeline is the same one a WebSocket feeds.

const (
	streamIdle   = 15 * time.Second // no audio for this long: the console left
	streamLinger = 2 * time.Minute  // a finished session's reason stays readable
	maxChunk     = 256 << 10
)

type streamSession struct {
	id, room string
	audio    chan []byte
	stopRun  context.CancelCauseFunc

	mu      sync.Mutex
	last    time.Time
	closed  bool   // audio closed: no more chunks
	done    bool   // the pipeline finished
	outcome string // why it finished: ended, revoked, busy, error: …
}

// push queues a chunk; false once the session no longer takes audio.
func (ss *streamSession) push(b []byte) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return false
	}
	ss.last = time.Now()
	select {
	case ss.audio <- b:
	default: // the pipeline is behind: drop, as the WebSocket path does
	}
	return true
}

func (ss *streamSession) closeAudio() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if !ss.closed {
		ss.closed = true
		close(ss.audio)
	}
}

func (s *Server) streamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/rooms/{room}/stream", s.publisher(s.streamStart))
	mux.HandleFunc("POST /api/admin/rooms/{room}/stream/{sid}", s.publisher(s.streamChunk))
	mux.HandleFunc("DELETE /api/admin/rooms/{room}/stream/{sid}", s.publisher(s.streamStop))
}

func (s *Server) streamStart(w http.ResponseWriter, r *http.Request) {
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
	b := make([]byte, 12)
	rand.Read(b)
	id := rm.Config().ID
	runCtx, stopRun := context.WithCancelCause(context.Background())
	ss := &streamSession{id: hex.EncodeToString(b), room: id, audio: make(chan []byte, 50), stopRun: stopRun, last: time.Now()}

	s.streamMu.Lock()
	if s.streams == nil {
		s.streams = map[string]*streamSession{}
	}
	s.streams[ss.id] = ss
	s.streamMu.Unlock()
	if s.who(r).role == rolePublisher { // revoking the speaker links cuts it off
		s.liveMu.Lock()
		s.live[id] = func() { stopRun(errRevoked) }
		s.liveMu.Unlock()
	}

	s.sessions.Add(1)
	done := make(chan error, 1)
	go func() { done <- rm.Run(runCtx, room.Transmission{Lang: lang, Talk: talk}, ss.audio) }()
	go s.superviseStream(rm, ss, runCtx, done)
	s.log.Info("operator connected over http", "room", id)
	writeJSON(w, map[string]string{"session": ss.id})
}

// superviseStream ends the audio when the console goes quiet, waits for
// the pipeline, and records why it ended.
func (s *Server) superviseStream(rm *room.Room, ss *streamSession, runCtx context.Context, done <-chan error) {
	defer s.sessions.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var runErr error
	finished := false
	for !finished {
		select {
		case runErr = <-done:
			finished = true
		case <-tick.C:
			ss.mu.Lock()
			idle := !ss.closed && time.Since(ss.last) > streamIdle
			ss.mu.Unlock()
			if idle {
				s.log.Info("operator went quiet; ending the http session", "room", ss.room)
				ss.closeAudio()
			}
		}
	}
	ss.closeAudio()
	outcome := "stopped"
	switch {
	case errors.Is(runErr, room.ErrBusy):
		outcome = "busy"
	case errors.Is(runErr, room.ErrEnded):
		outcome = "ended"
	case errors.Is(context.Cause(runCtx), errRevoked):
		outcome = "revoked"
	case runErr != nil && !errors.Is(runErr, context.Canceled):
		s.log.Error("room session failed", "room", ss.room, "err", runErr)
		outcome = "error: " + closeReason(runErr)
	}
	ss.mu.Lock()
	ss.done, ss.outcome = true, outcome
	ss.mu.Unlock()
	ss.stopRun(nil)
	s.liveMu.Lock()
	delete(s.live, ss.room)
	s.liveMu.Unlock()
	s.log.Info("operator disconnected", "room", ss.room, "outcome", outcome)
	time.AfterFunc(streamLinger, func() {
		s.streamMu.Lock()
		delete(s.streams, ss.id)
		s.streamMu.Unlock()
	})
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) (*streamSession, bool) {
	s.streamMu.Lock()
	ss, ok := s.streams[r.PathValue("sid")]
	s.streamMu.Unlock()
	if !ok || ss.room != r.PathValue("room") {
		http.Error(w, "no such session", http.StatusNotFound)
		return nil, false
	}
	return ss, true
}

func (s *Server) streamChunk(w http.ResponseWriter, r *http.Request) {
	ss, ok := s.stream(w, r)
	if !ok {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxChunk))
	if err != nil {
		http.Error(w, "audio chunk too large", http.StatusRequestEntityTooLarge)
		return
	}
	if len(data)%2 == 1 {
		data = data[:len(data)-1] // PCM16: whole samples only
	}
	if len(data) == 0 || ss.push(data) {
		ss.mu.Lock()
		done, outcome := ss.done, ss.outcome
		ss.mu.Unlock()
		if !done {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		streamGone(w, outcome)
		return
	}
	ss.mu.Lock()
	outcome := ss.outcome
	ss.mu.Unlock()
	streamGone(w, outcome)
}

func (s *Server) streamStop(w http.ResponseWriter, r *http.Request) {
	ss, ok := s.stream(w, r)
	if !ok {
		return
	}
	ss.closeAudio()
	w.WriteHeader(http.StatusNoContent)
}

// streamGone tells the console the session is over, and why.
func streamGone(w http.ResponseWriter, outcome string) {
	if outcome == "" {
		outcome = "stopped"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	json.NewEncoder(w).Encode(map[string]string{"reason": outcome})
}
