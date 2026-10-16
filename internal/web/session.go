package web

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const organiserCookie = "dubash_organiser"

func (s *Server) sessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		p := s.who(r)
		writeJSON(w, map[string]any{"organiser": p.role == roleAdmin, "development": s.access.admin == "", "google": s.google != nil})
	})
	mux.HandleFunc("POST /api/session", s.signIn)
	mux.HandleFunc("DELETE /api/session", s.signOut)
}
func (s *Server) sessionCookie(w http.ResponseWriter, r *http.Request, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: organiserCookie, Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: r.TLS != nil || s.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode})
}
func (s *Server) browserOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	base, err := url.Parse(s.baseURL(r))
	return err == nil && u.Scheme == base.Scheme && strings.EqualFold(u.Host, base.Host)
}
func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.browserOrigin(r) {
		http.Error(w, "This sign-in request came from another website.", 403)
		return
	}
	ip := clientIP(r, s.TrustProxy)
	if s.access.fails.blocked(ip) {
		http.Error(w, "Too many attempts. Try again in a few minutes.", 429)
		return
	}
	var input struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&input); err != nil {
		http.Error(w, "Enter your organiser access key.", 400)
		return
	}
	if s.access.admin != "" && subtle.ConstantTimeCompare([]byte(input.Key), []byte(s.access.admin)) != 1 {
		s.access.fails.fail(ip)
		http.Error(w, "That organiser access key was not recognised.", 401)
		return
	}
	expires := strconv.FormatInt(time.Now().Add(8*time.Hour).Unix(), 10)
	s.sessionCookie(w, r, expires+"."+s.access.sign("organiser-session", expires), 8*60*60)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) signOut(w http.ResponseWriter, r *http.Request) {
	if !s.browserOrigin(r) {
		http.Error(w, "Invalid origin.", 403)
		return
	}
	s.sessionCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) signedOrganiser(r *http.Request) bool {
	cookie, err := r.Cookie(organiserCookie)
	if err != nil {
		return false
	}
	expires, sig, ok := strings.Cut(cookie.Value, ".")
	if !ok {
		return false
	}
	n, err := strconv.ParseInt(expires, 10, 64)
	return err == nil && time.Now().Unix() <= n && s.access.valid(sig, "organiser-session", expires)
}
