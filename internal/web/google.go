// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sign in with Google, for organisers. It uses OpenID Connect's id_token
// flow posted back to us (response_mode=form_post), so the server needs
// only the public client ID, never a client secret: Google signs the ID
// token, and we check it against Google's published keys. Only verified
// emails listed in OrganiserEmails become organisers.

const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleCertsURL = "https://www.googleapis.com/oauth2/v3/certs"
	googleCallback = "/auth/google/callback"
	nonceTTL       = 10 * time.Minute
	handoffTTL     = time.Minute
)

// googleSignIn holds the client and the cached signing keys.
type googleSignIn struct {
	clientID string
	allowed  []string // lower-case emails

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
	fetch   func(context.Context) (map[string]*rsa.PublicKey, error) // replaced in tests

	usedMu sync.Mutex
	used   map[string]time.Time // handoff codes already spent
}

// UseGoogle enables Sign in with Google for the given organiser emails.
func (s *Server) UseGoogle(clientID string, emails []string) {
	if clientID == "" || len(emails) == 0 {
		s.google = nil
		return
	}
	g := &googleSignIn{clientID: clientID, used: map[string]time.Time{}, fetch: fetchGoogleKeys}
	for _, e := range emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			g.allowed = append(g.allowed, e)
		}
	}
	s.google = g
}

func (s *Server) googleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/google", s.googleStart)
	mux.HandleFunc("POST "+googleCallback, s.googleCallback)
	mux.HandleFunc("GET /auth/google/finish", s.googleFinish)
}

// googleStart sends the browser to Google's account chooser.
func (s *Server) googleStart(w http.ResponseWriter, r *http.Request) {
	g := s.google
	if g == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	n := s.newNonce(time.Now())
	q := url.Values{
		"client_id":     {g.clientID},
		"redirect_uri":  {s.baseURL(r) + googleCallback},
		"response_type": {"id_token"},
		"response_mode": {"form_post"},
		"scope":         {"openid email"},
		"nonce":         {n},
		"state":         {n},
		"prompt":        {"select_account"},
	}
	http.Redirect(w, r, googleAuthURL+"?"+q.Encode(), http.StatusSeeOther)
}

// googleCallback receives Google's post, checks the ID token and hands
// the browser on with a one-minute code. The session cookie is set on
// that next, same-site request: browsers may drop strict cookies set in
// answer to a cross-site post.
func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	g := s.google
	if g == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		googleProblem(w, http.StatusBadRequest, "Google sent an answer we could not read.")
		return
	}
	if e := r.PostForm.Get("error"); e != "" {
		googleProblem(w, http.StatusForbidden, "Google sign-in was cancelled ("+e+").")
		return
	}
	state := r.PostForm.Get("state")
	if !s.validNonce(state, time.Now()) {
		googleProblem(w, http.StatusForbidden, "This sign-in attempt expired. Please start again.")
		return
	}
	email, err := g.verify(r.Context(), r.PostForm.Get("id_token"), state, time.Now())
	if err != nil {
		s.log.Warn("google sign-in refused", "err", err)
		googleProblem(w, http.StatusForbidden, "Google could not confirm this sign-in. Please try again.")
		return
	}
	if !slices.Contains(g.allowed, email) {
		s.log.Warn("google sign-in: not an organiser", "email", email)
		googleProblem(w, http.StatusForbidden, html.EscapeString(email)+" is not an organiser of this Dubash. Ask the person who set it up to add you.")
		return
	}
	code := s.handoff(email, time.Now())
	s.log.Info("organiser signed in with google", "email", email)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=/auth/google/finish?code=%s"><title>Signing in · Dubash</title><p>Signing you in…</p>`, url.QueryEscape(code))
}

// googleFinish turns a valid handoff code into the organiser session.
func (s *Server) googleFinish(w http.ResponseWriter, r *http.Request) {
	g := s.google
	code := r.URL.Query().Get("code")
	if g == nil || !s.validHandoff(code, time.Now()) || !g.spend(code, time.Now()) {
		http.Redirect(w, r, "/login?error=expired", http.StatusSeeOther)
		return
	}
	expires := strconv.FormatInt(time.Now().Add(8*time.Hour).Unix(), 10)
	s.sessionCookie(w, r, expires+"."+s.access.sign("organiser-session", expires), 8*60*60)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func googleProblem(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Sign in · Dubash</title><link rel="stylesheet" href="/static/css/site.css"><body class="site"><main class="cta" style="margin-top:80px"><h2>Couldn’t sign you in</h2><p style="margin:0 0 28px">%s</p><a class="pill pill-glow" href="/login">Back to sign in</a></main></body>`, msg)
}

// newNonce is random, dated and signed, so the server keeps no state:
// "<random>.<expiry>.<signature>".
func (s *Server) newNonce(now time.Time) string {
	b := make([]byte, 16)
	rand.Read(b)
	r, exp := hex.EncodeToString(b), strconv.FormatInt(now.Add(nonceTTL).Unix(), 10)
	return r + "." + exp + "." + s.access.sign("google-nonce", r, exp)
}

func (s *Server) validNonce(n string, now time.Time) bool {
	p := strings.Split(n, ".")
	if len(p) != 3 {
		return false
	}
	exp, err := strconv.ParseInt(p[1], 10, 64)
	return err == nil && now.Unix() <= exp && s.access.valid(p[2], "google-nonce", p[0], p[1])
}

func (s *Server) handoff(email string, now time.Time) string {
	b := make([]byte, 12)
	rand.Read(b)
	e := base64.RawURLEncoding.EncodeToString([]byte(email))
	r, exp := hex.EncodeToString(b), strconv.FormatInt(now.Add(handoffTTL).Unix(), 10)
	return e + "." + r + "." + exp + "." + s.access.sign("google-handoff", e, r, exp)
}

func (s *Server) validHandoff(code string, now time.Time) bool {
	p := strings.Split(code, ".")
	if len(p) != 4 {
		return false
	}
	exp, err := strconv.ParseInt(p[2], 10, 64)
	return err == nil && now.Unix() <= exp && s.access.valid(p[3], "google-handoff", p[0], p[1], p[2])
}

// spend marks a handoff code used; a second use fails.
func (g *googleSignIn) spend(code string, now time.Time) bool {
	g.usedMu.Lock()
	defer g.usedMu.Unlock()
	for c, t := range g.used {
		if now.Sub(t) > 2*handoffTTL {
			delete(g.used, c)
		}
	}
	if _, ok := g.used[code]; ok {
		return false
	}
	g.used[code] = now
	return true
}

// verify checks an ID token's signature and claims and returns its email.
func (g *googleSignIn) verify(ctx context.Context, token, nonce string, now time.Time) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("not a JWT")
	}
	var header struct {
		Alg, Kid string
	}
	if err := decodeSegment(parts[0], &header); err != nil || header.Alg != "RS256" {
		return "", fmt.Errorf("unsupported token header %+v (%v)", header, err)
	}
	key, err := g.key(ctx, header.Kid)
	if err != nil {
		return "", err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return "", fmt.Errorf("bad signature: %w", err)
	}
	var c struct {
		Iss           string          `json:"iss"`
		Aud           string          `json:"aud"`
		Exp           int64           `json:"exp"`
		Iat           int64           `json:"iat"`
		Nonce         string          `json:"nonce"`
		Email         string          `json:"email"`
		EmailVerified json.RawMessage `json:"email_verified"` // true or "true"
	}
	if err := decodeSegment(parts[1], &c); err != nil {
		return "", err
	}
	switch {
	case c.Iss != "accounts.google.com" && c.Iss != "https://accounts.google.com":
		return "", fmt.Errorf("issuer %q", c.Iss)
	case c.Aud != g.clientID:
		return "", errors.New("token for another client")
	case now.Unix() > c.Exp || c.Iat > now.Add(5*time.Minute).Unix():
		return "", errors.New("token expired or from the future")
	case c.Nonce != nonce:
		return "", errors.New("nonce mismatch")
	case strings.Trim(string(c.EmailVerified), `"`) != "true" || c.Email == "":
		return "", errors.New("email not verified")
	}
	return strings.ToLower(c.Email), nil
}

// key returns Google's signing key kid, refreshing the cache hourly or
// when a new key id appears.
func (g *googleSignIn) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if k, ok := g.keys[kid]; ok && time.Since(g.fetched) < time.Hour {
		return k, nil
	}
	if time.Since(g.fetched) > 10*time.Second { // no refetch storms on bad ids
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		keys, err := g.fetch(ctx)
		if err != nil {
			return nil, fmt.Errorf("google keys: %w", err)
		}
		g.keys, g.fetched = keys, time.Now()
	}
	if k, ok := g.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("unknown signing key %q", kid)
}

func fetchGoogleKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, googleCertsURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var set struct {
		Keys []struct{ Kid, Kty, N, E string }
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&set); err != nil {
		return nil, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(keys) == 0 {
		return nil, errors.New("no RSA keys")
	}
	return keys, nil
}

func decodeSegment(seg string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
