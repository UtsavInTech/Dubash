// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/room"
)

func (s *Server) accessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /j/{code}", s.join)
	mux.HandleFunc("GET /api/rooms/{room}/access", s.roomAccess)
	mux.HandleFunc("POST /api/rooms/{room}/unlock", s.unlock)
	mux.HandleFunc("POST /api/admin/rooms/{room}/publisher-tokens", s.auth(s.mintPublisher))
	mux.HandleFunc("POST /api/admin/rooms/{room}/publisher-tokens/revoke", s.auth(s.revokePublishers))
	mux.HandleFunc("DELETE /api/admin/rooms/{room}/transcript", s.auth(s.deleteTranscript))
}

// secure sets the headers every response gets: no MIME sniffing, no
// referrer (a page URL never leaks to another site), a content policy
// that allows only this server's own scripts, and no framing of the
// staff pages (the caption pages may be embedded, for a venue's site or
// a streaming overlay).
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		csp := "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; object-src 'none'; base-uri 'none'; form-action 'self'"
		if staffPage(r.URL.Path) {
			csp += "; frame-ancestors 'none'"
			h.Set("X-Frame-Options", "DENY")
			h.Set("Cache-Control", "no-store")
		}
		h.Set("Content-Security-Policy", csp)
		if r.TLS != nil || s.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func staffPage(path string) bool {
	for _, p := range []string{"/admin", "/bank", "/operator/", "/api/admin/", "/metrics", "/ingest/"} {
		if path == p || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// mintPublisher gives the organiser a token for one event's speaker
// console, and the console link carrying it in the URL fragment (which
// browsers never send to a server, so it stays out of logs).
func (s *Server) mintPublisher(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	var req struct {
		Hours float64 `json:"hours"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
			http.Error(w, "body must be JSON: {\"hours\": 12}", http.StatusBadRequest)
			return
		}
	}
	ttl := time.Duration(req.Hours * float64(time.Hour))
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	if ttl > maxPublisherTTL {
		http.Error(w, "a speaker link lasts at most 72 hours", http.StatusBadRequest)
		return
	}
	id := rm.Config().ID
	now := time.Now()
	tok := s.access.publisherToken(id, ttl, now)
	s.log.Info("speaker link issued", "room", id, "expires", now.Add(ttl).Format(time.RFC3339))
	writeJSON(w, map[string]any{
		"token":   tok,
		"expires": now.Add(ttl).UTC(),
		"console": s.baseURL(r) + "/operator/" + id + "#token=" + tok,
	})
}

// revokePublishers ends every speaker link issued for the event so far
// (a lost phone, a speaker who left); a console already live is cut off.
func (s *Server) revokePublishers(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	if err := s.access.revoke(rm.Config().ID, time.Now()); err != nil {
		http.Error(w, "could not save the revocation: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.cutPublisher(rm)
	s.log.Info("speaker links revoked", "room", rm.Config().ID)
	w.WriteHeader(http.StatusNoContent)
}

// cutPublisher closes the room's live console when a speaker link opened
// it (the organiser's own console stays).
func (s *Server) cutPublisher(rm *room.Room) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	if cut, ok := s.live[rm.Config().ID]; ok {
		cut()
	}
}

// deleteTranscript removes an event's stored lines, its live fixes and
// the history new readers would get. It refuses while the event is live.
func (s *Server) deleteTranscript(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	if rm.Status().Live {
		http.Error(w, "the event is live: end it first", http.StatusConflict)
		return
	}
	n, err := s.purge(r.Context(), rm, time.Now().Add(time.Minute), true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"fixes": n})
}

// purge deletes rm's lines and live fixes from before cutoff (all of
// them with all), returning how many fixes went.
func (s *Server) purge(ctx context.Context, rm *room.Room, cutoff time.Time, all bool) (int, error) {
	id := rm.Config().ID
	if all {
		if err := s.store.Delete(id); err != nil {
			return 0, err
		}
		rm.Hub().Forget()
	} else {
		if _, err := s.store.Purge(id, cutoff); err != nil {
			return 0, err
		}
		rm.Hub().ForgetBefore(cutoff)
	}
	if s.Bank == nil {
		return 0, nil
	}
	return s.Bank.PurgeCorrections(ctx, id, cutoff, "retention")
}

// Retain applies each room's retention_days every hour until ctx ends.
func (s *Server) Retain(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		s.retainOnce(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) retainOnce(ctx context.Context, now time.Time) {
	for _, rm := range s.order {
		days := rm.Config().RetentionDays
		if days <= 0 {
			continue
		}
		cutoff := now.AddDate(0, 0, -days)
		if n, err := s.purge(ctx, rm, cutoff, false); err != nil {
			s.log.Warn("retention purge failed", "room", rm.Config().ID, "err", err)
		} else if n > 0 {
			s.log.Info("retention purge", "room", rm.Config().ID, "fixes", n)
		}
	}
}

// canRead reports whether the caller may read rm's captions: anyone when
// the event has no PIN; else staff, or a reader who entered the PIN or
// scanned the QR code.
func (s *Server) canRead(rm *room.Room, r *http.Request) bool {
	cfg := rm.Config()
	if cfg.AudiencePIN == "" {
		return true
	}
	if p := s.who(r); p.role == roleAdmin || p.role == rolePublisher && p.room == cfg.ID {
		return true
	}
	c, err := r.Cookie(readerCookie + cfg.ID)
	return err == nil && s.access.validReader(cfg.ID, c.Value, time.Now())
}

// canExport applies the event's exports policy.
func (s *Server) canExport(rm *room.Room, r *http.Request) bool {
	switch rm.Config().ExportPolicy() {
	case config.ExportOff:
		return false
	case config.ExportAdmin:
		p := s.who(r)
		return p.role == roleAdmin || p.role == rolePublisher && p.room == rm.Config().ID
	}
	return s.canRead(rm, r)
}

// readers admits those who can read the request's room.
func (s *Server) readers(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rm, ok := s.rooms[r.PathValue("room")]; ok && !s.canRead(rm, r) {
			http.Error(w, "this event asks for a PIN", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) roomAccess(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	writeJSON(w, map[string]bool{
		"pin":     rm.Config().AudiencePIN != "",
		"allowed": s.canRead(rm, r),
		"exports": s.canExport(rm, r),
	})
}

// unlock checks a reader's PIN and gives them the event's reader cookie.
// Wrong PINs count towards the same lockout as wrong tokens.
func (s *Server) unlock(w http.ResponseWriter, r *http.Request) {
	rm, ok := s.room(w, r)
	if !ok {
		return
	}
	ip := clientIP(r, s.TrustProxy)
	if s.access.fails.blocked(ip) {
		http.Error(w, "too many wrong PINs; try again in a few minutes", http.StatusTooManyRequests)
		return
	}
	var req struct {
		PIN string `json:"pin"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req)
	pin := rm.Config().AudiencePIN
	if pin == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.PIN)), []byte(pin)) != 1 {
		s.access.fails.fail(ip)
		http.Error(w, "wrong PIN", http.StatusForbidden)
		return
	}
	s.admit(w, r, rm.Config().ID)
	w.WriteHeader(http.StatusNoContent)
}

// admit sets the reader cookie for room.
func (s *Server) admit(w http.ResponseWriter, r *http.Request, id string) {
	http.SetCookie(w, &http.Cookie{
		Name: readerCookie + id, Value: s.access.readerToken(id, time.Now()), Path: "/",
		MaxAge: int(readerTTL / time.Second), HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil || s.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

// join is the QR code's target. Scanning it in the venue admits the
// reader without the PIN; an old code (the room's join_revision changed)
// no longer works.
func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(r.PathValue("code"))
	for _, rm := range s.order {
		cfg := rm.Config()
		if s.access.joinCode(cfg.ID, cfg.JoinRevision) != code {
			continue
		}
		if cfg.AudiencePIN != "" {
			s.admit(w, r, cfg.ID)
		}
		target := "/r/" + cfg.ID
		if l := r.URL.Query().Get("lang"); l != "" {
			target += "?lang=" + l
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	http.Error(w, "this code is not valid any more: ask the organisers for the current one", http.StatusNotFound)
}

// viewerSlot takes a caption connection slot for ip, or reports that the
// server or this address has too many.
func (s *Server) viewerSlot(ip string) (release func(), ok bool) {
	s.viewersMu.Lock()
	defer s.viewersMu.Unlock()
	if s.MaxViewers > 0 && s.viewers >= s.MaxViewers || s.MaxViewersPerIP > 0 && s.viewersIP[ip] >= s.MaxViewersPerIP {
		return nil, false
	}
	s.viewers++
	s.viewersIP[ip]++
	return func() {
		s.viewersMu.Lock()
		defer s.viewersMu.Unlock()
		s.viewers--
		if s.viewersIP[ip]--; s.viewersIP[ip] <= 0 {
			delete(s.viewersIP, ip)
		}
	}, true
}

// retryAfter tells a client to come back later.
func retryAfter(w http.ResponseWriter, secs int, msg string) {
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	http.Error(w, msg, http.StatusServiceUnavailable)
}
