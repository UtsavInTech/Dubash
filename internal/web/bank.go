// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/UtsavInTech/dubash/internal/bank"
	"github.com/UtsavInTech/dubash/internal/room"
)

// actor names whoever made a change. Until accounts exist (phase 4) every
// holder of the admin token is "admin".
const actor = "admin"

// maxCorrection bounds a corrected line, in bytes.
const maxCorrection = 2000

func (s *Server) bankRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /bank", s.page("bank.html"))
	mux.HandleFunc("GET /api/admin/bank/entries", s.auth(s.withBank(s.listEntries)))
	mux.HandleFunc("POST /api/admin/bank/entries", s.auth(s.withBank(s.saveEntry)))
	mux.HandleFunc("PUT /api/admin/bank/entries/{id}", s.auth(s.withBank(s.saveEntry)))
	mux.HandleFunc("GET /api/admin/bank/entries/{id}/history", s.auth(s.withBank(s.entryHistory)))
	mux.HandleFunc("POST /api/admin/bank/entries/{id}/approve", s.auth(s.withBank(s.approveEntry)))
	mux.HandleFunc("POST /api/admin/bank/entries/{id}/reject", s.auth(s.withBank(s.setStatus(bank.Rejected))))
	mux.HandleFunc("POST /api/admin/bank/entries/{id}/retire", s.auth(s.withBank(s.setStatus(bank.Retired))))
	mux.HandleFunc("GET /api/admin/bank/corrections", s.auth(s.withBank(s.listCorrections)))
	mux.HandleFunc("GET /api/admin/bank/audit", s.auth(s.withBank(s.audit)))
	mux.HandleFunc("GET /api/admin/events/{room}/context", s.auth(s.withBank(s.eventContext)))
	mux.HandleFunc("POST /api/admin/events/{room}/context", s.auth(s.withBank(s.publishContext)))
	mux.HandleFunc("POST /api/admin/events/{room}/context/{id}/pin", s.auth(s.withBank(s.pinContext)))
	mux.HandleFunc("POST /api/admin/rooms/{room}/segments/{id}/correct", s.auth(s.correct))
}

// withBank answers 503 when no bank is configured.
func (s *Server) withBank(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Bank == nil {
			http.Error(w, "the Dictation Bank is not configured", http.StatusServiceUnavailable)
			return
		}
		next(w, r)
	}
}

// bankError maps a bank error to an HTTP status.
func bankError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, bank.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case strings.HasPrefix(err.Error(), "bank: ") && !strings.Contains(err.Error(), "connect"):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) listEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	es, err := s.Bank.Entries(r.Context(), bank.Filter{
		Scope: bank.Scope(q.Get("scope")), ScopeID: q.Get("scopeId"), Status: bank.Status(q.Get("status")), Query: q.Get("q"),
	})
	if err != nil {
		bankError(w, err)
		return
	}
	if es == nil {
		es = []bank.Entry{}
	}
	writeJSON(w, es)
}

// saveEntry creates an entry (POST) or a new version of one (PUT).
func (s *Server) saveEntry(w http.ResponseWriter, r *http.Request) {
	var e bank.Entry
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&e); err != nil {
		http.Error(w, "invalid entry: "+err.Error(), http.StatusBadRequest)
		return
	}
	e.ID = r.PathValue("id") // empty for POST
	if e.Source == "" {
		e.Source = bank.SourceManual
	}
	saved, err := s.Bank.SaveEntry(r.Context(), e, actor)
	if err != nil {
		bankError(w, err)
		return
	}
	writeJSON(w, saved)
}

func (s *Server) entryHistory(w http.ResponseWriter, r *http.Request) {
	h, err := s.Bank.History(r.Context(), r.PathValue("id"))
	if err != nil {
		bankError(w, err)
		return
	}
	writeJSON(w, h)
}

func (s *Server) approveEntry(w http.ResponseWriter, r *http.Request) {
	e, err := bank.Approve(r.Context(), s.Bank, r.PathValue("id"), actor)
	if err != nil {
		bankError(w, err)
		return
	}
	writeJSON(w, e)
}

func (s *Server) setStatus(st bank.Status) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		e, err := s.Bank.Entry(r.Context(), r.PathValue("id"))
		if err != nil {
			bankError(w, err)
			return
		}
		e.Status = st
		saved, err := s.Bank.SaveEntry(r.Context(), e, actor)
		if err != nil {
			bankError(w, err)
			return
		}
		writeJSON(w, saved)
	}
}

func (s *Server) listCorrections(w http.ResponseWriter, r *http.Request) {
	cs, err := s.Bank.Corrections(r.Context(), r.URL.Query().Get("event"))
	if err != nil {
		bankError(w, err)
		return
	}
	if cs == nil {
		cs = []bank.Correction{}
	}
	writeJSON(w, cs)
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	a, err := s.Bank.Audit(r.Context(), n)
	if err != nil {
		bankError(w, err)
		return
	}
	if a == nil {
		a = []bank.AuditEntry{}
	}
	writeJSON(w, a)
}

// contextView describes an event's Dictation Bank state for the admin:
// the version in force, the versions saved, and what publishing now would
// change.
type contextView struct {
	Event    string         `json:"event"`
	Speaker  string         `json:"speaker,omitempty"`
	InForce  int            `json:"inForce"` // version number the room uses (0: none)
	Pinned   *bank.Version  `json:"pinned,omitempty"`
	Versions []bank.Version `json:"versions"`
	// Pending counts the differences between the pinned version and what
	// publishing now would snapshot.
	Pending pending `json:"pending"`
}

type pending struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Changed []string `json:"changed"`
}

func compare(old, now []bank.Entry) pending {
	p := pending{Added: []string{}, Removed: []string{}, Changed: []string{}}
	key := func(e bank.Entry) string { return e.Lang + "\x00" + bank.Fold(e.Term) }
	before := map[string]bank.Entry{}
	for _, e := range old {
		before[key(e)] = e
	}
	seen := map[string]bool{}
	for _, e := range now {
		k := key(e)
		seen[k] = true
		o, ok := before[k]
		switch {
		case !ok:
			p.Added = append(p.Added, e.Term)
		case o.ID != e.ID || o.Version != e.Version:
			p.Changed = append(p.Changed, e.Term)
		}
	}
	for k, e := range before {
		if !seen[k] {
			p.Removed = append(p.Removed, e.Term)
		}
	}
	return p
}

func (s *Server) eventContext(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	cfg := rm.Config()
	view := contextView{Event: cfg.ID, Speaker: cfg.Speaker, InForce: rm.Context().Version.Number}
	pinned, ok, err := s.Bank.Pinned(ctx, cfg.ID)
	if err != nil {
		bankError(w, err)
		return
	}
	var old []bank.Entry
	if ok {
		view.Pinned, old = &pinned, pinned.Entries
	}
	if view.Versions, err = s.Bank.Versions(ctx, cfg.ID); err != nil {
		bankError(w, err)
		return
	}
	if view.Versions == nil {
		view.Versions = []bank.Version{}
	}
	all, err := s.Bank.Entries(ctx, bank.Filter{Status: bank.Approved})
	if err != nil {
		bankError(w, err)
		return
	}
	view.Pending = compare(old, bank.Resolve(all, cfg.ID, cfg.Speaker))
	writeJSON(w, view)
}

// publishContext snapshots the approved entries for the event as a new
// version, pins it and puts it in force from the next committed line.
func (s *Server) publishContext(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	note := strings.TrimSpace(r.FormValue("note"))
	if utf8.RuneCountInString(note) > 200 {
		http.Error(w, "note too long", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	cfg := rm.Config()
	all, err := s.Bank.Entries(ctx, bank.Filter{Status: bank.Approved})
	if err != nil {
		bankError(w, err)
		return
	}
	v, err := s.Bank.SaveVersion(ctx, bank.NewVersion(all, cfg.ID, cfg.Speaker, actor, note), actor)
	if err != nil {
		bankError(w, err)
		return
	}
	if err := s.Bank.Pin(ctx, cfg.ID, v.ID, actor); err != nil {
		bankError(w, err)
		return
	}
	rm.SetVersion(v)
	s.log.Info("dictation bank version published", "room", cfg.ID, "version", v.Number, "entries", len(v.Entries))
	writeJSON(w, v)
}

// pinContext puts an earlier version back in force (a rollback).
func (s *Server) pinContext(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad version id", http.StatusBadRequest)
		return
	}
	v, err := s.Bank.Version(r.Context(), id)
	if err == nil && v.Event != rm.Config().ID {
		err = bank.ErrNotFound
	}
	if err == nil {
		err = s.Bank.Pin(r.Context(), v.Event, v.ID, actor)
	}
	if err != nil {
		bankError(w, err)
		return
	}
	rm.SetVersion(v)
	writeJSON(w, v)
}

// correctResponse is the answer to a live word fix.
type correctResponse struct {
	Before    any      `json:"before"`
	After     any      `json:"after"`
	Suggested []string `json:"suggested"`
	Warning   string   `json:"warning,omitempty"`
}

// correct applies a live word fix: readers get the new revision at once;
// the fix is recorded and its replaced words suggested to the bank. A bank
// failure does not undo the fix (the live path never waits on it).
func (s *Server) correct(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "bad line id", http.StatusBadRequest)
		return
	}
	text := r.FormValue("text")
	if len(text) > maxCorrection {
		http.Error(w, "text too long", http.StatusBadRequest)
		return
	}
	before, after, err := rm.Correct(r.Context(), id, text)
	switch {
	case errors.Is(err, room.ErrUnknownSegment):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := correctResponse{Before: before, After: after, Suggested: []string{}}
	if after.Rev != before.Rev && s.Bank != nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
		defer cancel()
		c := bank.Correction{Event: after.Room, Segment: id, Rev: after.Rev, Raw: before.Text, Corrected: after.Text,
			Lang: after.Lang, At: time.Now().UTC(), By: actor}
		ids, err := bank.SuggestFrom(ctx, s.Bank, c, actor)
		if err == nil {
			c.Suggested = ids
			_, err = s.Bank.SaveCorrection(ctx, c)
		}
		if err != nil {
			s.log.Warn("could not record a correction in the bank", "room", after.Room, "err", err)
			resp.Warning = "the fix is live, but the bank could not record it: " + err.Error()
		}
		resp.Suggested = ids
	}
	writeJSON(w, resp)
}
