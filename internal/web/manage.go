package web

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/room"
	"gopkg.in/yaml.v3"
)

// Room creation is serialized separately from reads: persistence may take time,
// but existing caption streams and room lookups must remain available.
func (s *Server) roomSnapshot() []*room.Room {
	s.roomsMu.RLock()
	defer s.roomsMu.RUnlock()
	return slices.Clone(s.order)
}
func (s *Server) findRoom(id string) (*room.Room, bool) {
	s.roomsMu.RLock()
	defer s.roomsMu.RUnlock()
	rm, ok := s.rooms[id]
	return rm, ok
}
func (s *Server) catalogSnapshot() []config.Room {
	s.roomsMu.RLock()
	defer s.roomsMu.RUnlock()
	return slices.Clone(s.Catalog)
}

func (s *Server) manageRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/events", s.auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"create": s.CreateEvent != nil, "reason": "New events on a multi-server deployment need a room owner assigned by the deployment administrator."})
	}))
	mux.HandleFunc("POST /api/admin/events", s.auth(s.createEvent))
	mux.HandleFunc("GET /api/admin/rooms/{room}/links", s.publisher(s.eventLinks))
}

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	if s.CreateEvent == nil {
		http.Error(w, "Event creation is not enabled on this deployment. Ask the deployment administrator to assign a room owner.", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Title         string   `json:"title"`
		ID            string   `json:"id"`
		Speaker       string   `json:"speaker"`
		Source        string   `json:"source"`
		Targets       []string `json:"targets"`
		AudiencePIN   string   `json:"audiencePIN"`
		RetentionDays int      `json:"retentionDays"`
		Exports       string   `json:"exports"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "Please provide valid event details.", http.StatusBadRequest)
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Speaker = strings.TrimSpace(req.Speaker)
	if req.Title == "" || utf8.RuneCountInString(req.Title) > 160 || len(req.ID) > 80 || utf8.RuneCountInString(req.Speaker) > 160 {
		http.Error(w, "Use an event name of 1–160 characters and a link name of at most 80 characters.", http.StatusBadRequest)
		return
	}
	rc := config.Room{ID: req.ID, Title: req.Title, Speaker: req.Speaker, Source: req.Source, Targets: req.Targets, AudiencePIN: req.AudiencePIN, RetentionDays: req.RetentionDays, Exports: req.Exports}
	data, err := yaml.Marshal(config.Config{Rooms: []config.Room{rc}})
	if err != nil {
		http.Error(w, "Invalid event details.", http.StatusBadRequest)
		return
	}
	cfg, err := config.Parse(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rc = cfg.Rooms[0]
	s.createMu.Lock()
	defer s.createMu.Unlock()
	if _, ok := s.findRoom(rc.ID); ok {
		http.Error(w, "That event link is already taken. Choose another link name.", http.StatusConflict)
		return
	}
	rm, err := s.CreateEvent(r.Context(), rc)
	if err != nil {
		s.log.Error("create event", "err", err)
		http.Error(w, "The event could not be saved. Please try again.", http.StatusInternalServerError)
		return
	}
	if rm == nil || rm.Config().ID != rc.ID {
		http.Error(w, "Invalid event factory result.", http.StatusInternalServerError)
		return
	}
	s.roomsMu.Lock()
	s.rooms[rc.ID] = rm
	s.order = append(s.order, rm)
	if s.Catalog != nil {
		s.Catalog = append(s.Catalog, rc)
	}
	s.roomsMu.Unlock()
	s.log.Info("event created", "room", rc.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, rm.Status())
}

func (s *Server) eventLinks(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	id := rm.Config().ID
	if rm.Config().AudiencePIN != "" {
		s.admit(w, r, id)
	}
	writeJSON(w, map[string]string{
		"audience": s.baseURL(r) + "/r/" + id,
		"join":     s.baseURL(r) + "/j/" + s.access.joinCode(id, rm.Config().JoinRevision),
		"console":  s.baseURL(r) + "/operator/" + id,
	})
}

// EventFactory persists a new event before exposing its live room.
type EventFactory func(context.Context, config.Room) (*room.Room, error)
