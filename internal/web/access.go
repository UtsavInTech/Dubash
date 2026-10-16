// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Roles, from the brief: the organiser (the admin token), a publisher
// (the speaker console of one event, with a token the organiser mints),
// and listeners (the audience, with no token, or a reader cookie when the
// event has a PIN).
type role int

const (
	roleNone role = iota
	rolePublisher
	roleAdmin
)

type principal struct {
	role role
	room string // a publisher's event
}

// Token kinds and their lifetimes.
const (
	publisherPrefix = "p1."
	readerPrefix    = "a1."
	maxPublisherTTL = 72 * time.Hour
	readerTTL       = 12 * time.Hour
	readerCookie    = "dubash_reader_"
	wsTokenProto    = "dubash-token." // a WebSocket subprotocol carrying the token
	wsProto         = "dubash"        // the subprotocol the server answers with
)

// access holds what the web layer needs to tell roles apart: the signing
// key, revocations, and the failed-attempt limiter.
type access struct {
	admin string // ADMIN_TOKEN; empty disables auth (development only)
	key   []byte // signs publisher tokens, reader cookies and join codes

	mu      sync.Mutex
	revoked map[string]int64 // room → publisher tokens issued before this (unix s) are revoked
	path    string           // where revocations persist (empty: memory only)

	fails *limiter
}

func newAccess(admin, secret, stateDir string) *access {
	if secret == "" {
		secret = admin
	}
	mac := hmac.New(sha256.New, []byte("dubash access key v1"))
	mac.Write([]byte(secret))
	a := &access{admin: admin, key: mac.Sum(nil), revoked: map[string]int64{}, fails: newLimiter(10, 5*time.Minute, 5*time.Minute)}
	if stateDir != "" {
		a.path = filepath.Join(stateDir, "access.json")
		if data, err := os.ReadFile(a.path); err == nil {
			json.Unmarshal(data, &a.revoked)
		}
	}
	return a
}

func (a *access) sign(parts ...string) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(strings.Join(parts, "\x00")))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18])
}

func (a *access) valid(sig string, parts ...string) bool {
	return hmac.Equal([]byte(sig), []byte(a.sign(parts...)))
}

// publisherToken mints a token for room's speaker console, valid for ttl.
func (a *access) publisherToken(room string, ttl time.Duration, now time.Time) string {
	r := base64.RawURLEncoding.EncodeToString([]byte(room))
	iat, exp := strconv.FormatInt(now.Unix(), 10), strconv.FormatInt(now.Add(ttl).Unix(), 10)
	return publisherPrefix + r + "." + iat + "." + exp + "." + a.sign("publisher", room, iat, exp)
}

// parsePublisher returns the room of a valid, unexpired, unrevoked
// publisher token.
func (a *access) parsePublisher(tok string, now time.Time) (string, bool) {
	if !strings.HasPrefix(tok, publisherPrefix) {
		return "", false
	}
	p := strings.Split(strings.TrimPrefix(tok, publisherPrefix), ".")
	if len(p) != 4 {
		return "", false
	}
	rb, err := base64.RawURLEncoding.DecodeString(p[0])
	if err != nil {
		return "", false
	}
	room := string(rb)
	iat, err1 := strconv.ParseInt(p[1], 10, 64)
	exp, err2 := strconv.ParseInt(p[2], 10, 64)
	if err1 != nil || err2 != nil || !a.valid(p[3], "publisher", room, p[1], p[2]) || now.Unix() > exp {
		return "", false
	}
	a.mu.Lock()
	min := a.revoked[room]
	a.mu.Unlock()
	if iat < min {
		return "", false
	}
	return room, true
}

// revoke invalidates every publisher token of room issued until now.
func (a *access) revoke(room string, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revoked[room] = now.Unix() + 1
	if a.path == "" {
		return nil
	}
	data, _ := json.Marshal(a.revoked)
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

// readerToken is a reader's pass for room, after its PIN.
func (a *access) readerToken(room string, now time.Time) string {
	exp := strconv.FormatInt(now.Add(readerTTL).Unix(), 10)
	return readerPrefix + exp + "." + a.sign("reader", room, exp)
}

func (a *access) validReader(room, tok string, now time.Time) bool {
	p := strings.Split(strings.TrimPrefix(tok, readerPrefix), ".")
	if !strings.HasPrefix(tok, readerPrefix) || len(p) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(p[0], 10, 64)
	return err == nil && now.Unix() <= exp && a.valid(p[1], "reader", room, p[0])
}

// joinCode is the opaque code of room's QR link (/j/{code}). Changing the
// room's join_revision in rooms.yaml changes it, so printed codes can be
// revoked.
func (a *access) joinCode(room string, revision int) string {
	mac := hmac.New(sha256.New, a.key)
	fmt.Fprintf(mac, "join\x00%s\x00%d", room, revision)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(mac.Sum(nil)[:7]))
}

// bearer returns the token a request carries: the Authorization header,
// the WebSocket subprotocol (browsers cannot set headers on WebSockets),
// or, for older clients, the token query parameter.
func bearer(r *http.Request) string {
	if key := r.Header.Get("X-Dubash-Token"); key != "" {
		return key
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	for _, p := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if p = strings.TrimSpace(p); strings.HasPrefix(p, wsTokenProto) {
			return strings.TrimPrefix(p, wsTokenProto)
		}
	}
	return r.URL.Query().Get("token")
}

// who identifies the caller.
func (s *Server) who(r *http.Request) principal {
	a := s.access
	if a.admin == "" {
		return principal{role: roleAdmin} // auth disabled (development)
	}
	tok := bearer(r)
	if tok == "" {
		if s.signedOrganiser(r) {
			return principal{role: roleAdmin}
		}
		return principal{}
	}
	if subtle.ConstantTimeCompare([]byte(tok), []byte(a.admin)) == 1 {
		return principal{role: roleAdmin}
	}
	if room, ok := a.parsePublisher(tok, time.Now()); ok {
		return principal{role: rolePublisher, room: room}
	}
	return principal{}
}

// guard admits callers whose principal passes ok; a caller that keeps
// failing is locked out for a while (no brute-forcing the token).
func (s *Server) guard(ok func(principal, *http.Request) bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, s.TrustProxy)
		if s.access.fails.blocked(ip) {
			http.Error(w, "too many failed attempts; try again in a few minutes", http.StatusTooManyRequests)
			return
		}
		p := s.who(r)
		if !ok(p, r) {
			if p.role == roleNone {
				s.access.fails.fail(ip)
				http.Error(w, "invalid token", http.StatusUnauthorized)
			} else {
				http.Error(w, "this token is not allowed here", http.StatusForbidden)
			}
			return
		}
		next(w, r)
	}
}

// auth admits the organiser only.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return s.guard(func(p principal, _ *http.Request) bool { return p.role == roleAdmin }, next)
}

// publisher admits the organiser, or the publisher of the request's room.
func (s *Server) publisher(next http.HandlerFunc) http.HandlerFunc {
	return s.guard(func(p principal, r *http.Request) bool {
		return p.role == roleAdmin || p.role == rolePublisher && p.room == r.PathValue("room")
	}, next)
}

// anyStaff admits the organiser and any publisher (whose answers the
// handler narrows to their event).
func (s *Server) anyStaff(next http.HandlerFunc) http.HandlerFunc {
	return s.guard(func(p principal, _ *http.Request) bool { return p.role != roleNone }, next)
}

// clientIP is the caller's address: the connection's, or with trustProxy
// the first X-Forwarded-For hop (only behind a proxy that sets it).
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			return strings.TrimSpace(strings.Split(f, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter counts failures per key in a window and blocks a key that
// reaches max, for block.
type limiter struct {
	mu            sync.Mutex
	max           int
	window, block time.Duration
	hits          map[string][]time.Time
	until         map[string]time.Time
}

func newLimiter(max int, window, block time.Duration) *limiter {
	return &limiter{max: max, window: window, block: block, hits: map[string][]time.Time{}, until: map[string]time.Time{}}
}

func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Now().Before(l.until[key])
}

func (l *limiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var recent []time.Time
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	if len(recent) >= l.max {
		l.until[key] = now.Add(l.block)
		recent = nil
	}
	l.hits[key] = recent
	if len(l.hits) > 10000 { // forget stale keys
		for k, ts := range l.hits {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) > l.window {
				delete(l.hits, k)
			}
		}
	}
}
