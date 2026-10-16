// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// googleToken signs an ID token as Google would, with key and kid.
func googleToken(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	head := enc(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"}) + "." + enc(claims)
	digest := sha256.Sum256([]byte(head))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return head + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestGoogleSignIn(t *testing.T) {
	site, srv := secureServer(t)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	site.UseGoogle("client-123.apps.googleusercontent.com", []string{"Organiser@Example.org"})
	site.google.fetch = func(context.Context) (map[string]*rsa.PublicKey, error) {
		return map[string]*rsa.PublicKey{"k1": &key.PublicKey}, nil
	}
	noFollow := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	// The start sends the browser to Google with a signed nonce.
	resp, err := (&http.Client{CheckRedirect: noFollow}).Get(srv.URL + "/auth/google")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	q := loc.Query()
	if loc.Host != "accounts.google.com" || q.Get("response_type") != "id_token" || q.Get("response_mode") != "form_post" ||
		q.Get("redirect_uri") != srv.URL+googleCallback || q.Get("client_id") != "client-123.apps.googleusercontent.com" {
		t.Fatalf("redirect %s", loc)
	}
	nonce := q.Get("nonce")
	claims := func(mod func(map[string]any)) map[string]any {
		c := map[string]any{"iss": "https://accounts.google.com", "aud": "client-123.apps.googleusercontent.com",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce,
			"email": "organiser@example.org", "email_verified": true}
		if mod != nil {
			mod(c)
		}
		return c
	}
	post := func(c *http.Client, token, state string) (*http.Response, string) {
		r, err := c.PostForm(srv.URL+googleCallback, url.Values{"id_token": {token}, "state": {state}})
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return r, b.String()
	}

	// Refused: another client, another person, unverified, wrong nonce,
	// forged signature, expired.
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	for name, tok := range map[string]string{
		"audience":   googleToken(t, key, "k1", claims(func(c map[string]any) { c["aud"] = "someone-else" })),
		"stranger":   googleToken(t, key, "k1", claims(func(c map[string]any) { c["email"] = "stranger@example.org" })),
		"unverified": googleToken(t, key, "k1", claims(func(c map[string]any) { c["email_verified"] = false })),
		"nonce":      googleToken(t, key, "k1", claims(func(c map[string]any) { c["nonce"] = "x" })),
		"forged":     googleToken(t, other, "k1", claims(nil)),
		"expired":    googleToken(t, key, "k1", claims(func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() })),
	} {
		if r, _ := post(http.DefaultClient, tok, nonce); r.StatusCode != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, r.StatusCode)
		}
	}
	if r, _ := post(http.DefaultClient, googleToken(t, key, "k1", claims(nil)), "made.up.state"); r.StatusCode != http.StatusForbidden {
		t.Errorf("forged state: %d", r.StatusCode)
	}

	// Accepted: the callback hands on to finish, which sets the session.
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: noFollow}
	r, body := post(browser, googleToken(t, key, "k1", claims(nil)), nonce)
	m := regexp.MustCompile(`url=(/auth/google/finish\?code=[^"]+)`).FindStringSubmatch(body)
	if r.StatusCode != http.StatusOK || m == nil {
		t.Fatalf("callback: %d %s", r.StatusCode, body)
	}
	finish, err := browser.Get(srv.URL + m[1])
	if err != nil {
		t.Fatal(err)
	}
	finish.Body.Close()
	if finish.StatusCode != http.StatusSeeOther || finish.Header.Get("Location") != "/admin" {
		t.Fatalf("finish: %d %s", finish.StatusCode, finish.Header.Get("Location"))
	}
	st, err := browser.Get(srv.URL + "/api/admin/status")
	if err != nil {
		t.Fatal(err)
	}
	st.Body.Close()
	if st.StatusCode != http.StatusOK {
		t.Errorf("organiser session after Google sign-in: %d", st.StatusCode)
	}
	// The handoff code works once.
	again, _ := (&http.Client{CheckRedirect: noFollow}).Get(srv.URL + m[1])
	again.Body.Close()
	if again.Header.Get("Location") != "/login?error=expired" {
		t.Errorf("reused handoff: %s", again.Header.Get("Location"))
	}
}

func TestGoogleOffWithoutOrganisers(t *testing.T) {
	site, _ := secureServer(t)
	site.UseGoogle("client", nil)
	if site.google != nil {
		t.Error("Google sign-in on with nobody allowed")
	}
}

func TestBaseURLBehindTLSProxy(t *testing.T) {
	site, _ := secureServer(t)
	r, _ := http.NewRequest(http.MethodGet, "http://example.org/", nil)
	r.Host = "dubash.example.org:443"
	if got := site.baseURL(r); got != "https://dubash.example.org" {
		t.Errorf("baseURL = %q", got)
	}
}
