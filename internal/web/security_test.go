// Copyright 2026 the Dubash contributors. Apache-2.0.

package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/bank"
	"github.com/UtsavInTech/dubash/internal/caption"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/translate"
)

// secureServer serves an open hall, a hall with a PIN whose transcript
// only staff may download, and a hall with downloads off.
func secureServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	var rooms []*room.Room
	for _, rc := range []config.Room{
		{ID: "open", Title: "Open", Source: "hi", Targets: []string{"en"}},
		{ID: "pin", Title: "PIN", Source: "hi", Targets: []string{"en"}, AudiencePIN: "4821", Exports: config.ExportAdmin, JoinRevision: 2},
		{ID: "off", Title: "Off", Source: "hi", Targets: []string{"en"}, Exports: config.ExportOff, RetentionDays: 30},
	} {
		rm, err := room.New(rc, room.Deps{ASR: asr.Fake{Every: 1}, Translator: translate.Fake{}, Store: st, Log: log})
		if err != nil {
			t.Fatal(err)
		}
		rooms = append(rooms, rm)
	}
	site := New(rooms, st, "admin-secret-of-enough-length", log)
	site.UseState(t.TempDir(), "")
	if site.Bank, err = bank.OpenFile(filepath.Join(t.TempDir(), "bank.json")); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(site.Handler())
	t.Cleanup(srv.Close)
	return site, srv
}

const adminTok = "admin-secret-of-enough-length"

func do(t *testing.T, c *http.Client, method, url, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func mint(t *testing.T, srv *httptest.Server, roomID string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/admin/rooms/"+roomID+"/publisher-tokens", strings.NewReader(`{"hours": 2}`))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct{ Token, Console string }
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil {
		t.Fatalf("mint: %d", resp.StatusCode)
	}
	if !strings.HasSuffix(out.Console, "/operator/"+roomID+"#token="+out.Token) {
		t.Errorf("console link %q must carry the token in the fragment", out.Console)
	}
	return out.Token
}

func TestPublisherTokenIsLimitedToItsEvent(t *testing.T) {
	_, srv := secureServer(t)
	tok := mint(t, srv, "open")

	if r := do(t, nil, http.MethodPost, srv.URL+"/api/admin/rooms/open/pause", tok, ""); r.StatusCode != http.StatusOK {
		t.Errorf("own event pause: %d", r.StatusCode)
	}
	do(t, nil, http.MethodPost, srv.URL+"/api/admin/rooms/open/resume", tok, "")
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/admin/rooms/pin/pause"},             // another event
		{http.MethodPost, "/api/admin/rooms/open/end"},              // organiser only
		{http.MethodPost, "/api/admin/rooms/open/publisher-tokens"}, // no minting more
		{http.MethodGet, "/api/admin/bank/entries"},
		{http.MethodGet, "/metrics"},
		{http.MethodDelete, "/api/admin/rooms/open/transcript"},
	} {
		if r := do(t, nil, c.method, srv.URL+c.path, tok, ""); r.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s with a speaker link: %d, want 403", c.method, c.path, r.StatusCode)
		}
	}
	// The status lists only the speaker's event.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/admin/status", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sts []room.Status
	json.NewDecoder(resp.Body).Decode(&sts)
	resp.Body.Close()
	if len(sts) != 1 || sts[0].ID != "open" {
		t.Errorf("a speaker sees %d events", len(sts))
	}

	// Revoked: the token stops working; a new one works.
	if r := do(t, nil, http.MethodPost, srv.URL+"/api/admin/rooms/open/publisher-tokens/revoke", adminTok, ""); r.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", r.StatusCode)
	}
	if r := do(t, nil, http.MethodPost, srv.URL+"/api/admin/rooms/open/pause", tok, ""); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoked token: %d", r.StatusCode)
	}
	time.Sleep(1100 * time.Millisecond) // tokens are dated to the second
	if r := do(t, nil, http.MethodPost, srv.URL+"/api/admin/rooms/open/resume", mint(t, srv, "open"), ""); r.StatusCode != http.StatusOK {
		t.Errorf("new token after revoking: %d", r.StatusCode)
	}
}

func TestPublisherTokens(t *testing.T) {
	a := newAccess("admin", "", t.TempDir())
	now := time.Now()
	tok := a.publisherToken("hall", time.Hour, now)
	if r, ok := a.parsePublisher(tok, now); !ok || r != "hall" {
		t.Fatalf("valid token: %q %v", r, ok)
	}
	if _, ok := a.parsePublisher(tok, now.Add(2*time.Hour)); ok {
		t.Error("expired token accepted")
	}
	forged := strings.Replace(tok, "aGFsbA", "b3RoZXI", 1) // hall → other
	if _, ok := a.parsePublisher(forged, now); ok {
		t.Error("token for another room accepted")
	}
	if _, ok := newAccess("other-admin", "", "").parsePublisher(tok, now); ok {
		t.Error("token signed with another key accepted")
	}
	// TOKEN_SECRET signs instead of the admin token: rotating the admin
	// token keeps speaker links.
	b := newAccess("admin", "a-separate-secret", "")
	if _, ok := newAccess("new-admin", "a-separate-secret", "").parsePublisher(b.publisherToken("hall", time.Hour, now), now); !ok {
		t.Error("speaker link lost with the admin token")
	}
	// Revocations survive a restart.
	dir := t.TempDir()
	c := newAccess("admin", "", dir)
	old := c.publisherToken("hall", time.Hour, now)
	c.revoke("hall", now)
	if _, ok := newAccess("admin", "", dir).parsePublisher(old, now); ok {
		t.Error("revocation forgotten after restart")
	}
}

func TestFailedAttemptsLockOut(t *testing.T) {
	_, srv := secureServer(t)
	for range 10 {
		do(t, nil, http.MethodGet, srv.URL+"/api/admin/status", "guess", "")
	}
	if r := do(t, nil, http.MethodGet, srv.URL+"/api/admin/status", adminTok, ""); r.StatusCode != http.StatusTooManyRequests {
		t.Errorf("after 10 failures the right token got %d, want 429", r.StatusCode)
	}
}

func TestAudiencePIN(t *testing.T) {
	site, srv := secureServer(t)
	jar, _ := cookiejar.New(nil)
	reader := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, path := range []string{"/events/pin", "/qr/pin", "/api/rooms/pin/talks"} {
		if r := do(t, reader, http.MethodGet, srv.URL+path, "", ""); r.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without the PIN: %d", path, r.StatusCode)
		}
	}
	if r := do(t, reader, http.MethodGet, srv.URL+"/qr/open", "", ""); r.StatusCode != http.StatusOK {
		t.Errorf("an open event's QR: %d", r.StatusCode)
	}
	if r := do(t, reader, http.MethodPost, srv.URL+"/api/rooms/pin/unlock", "", `{"pin":"0000"}`); r.StatusCode != http.StatusForbidden {
		t.Errorf("wrong PIN: %d", r.StatusCode)
	}
	if r := do(t, reader, http.MethodPost, srv.URL+"/api/rooms/pin/unlock", "", `{"pin":"4821"}`); r.StatusCode != http.StatusNoContent {
		t.Fatalf("right PIN: %d", r.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events/pin?lang=en", nil)
	resp, err := reader.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("events after the PIN: %d", resp.StatusCode)
	}
	cancel()
	resp.Body.Close()
	// exports: admin, so a reader with the PIN still cannot download.
	if r := do(t, reader, http.MethodGet, srv.URL+"/export/pin/txt", "", ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("reader export of an admin-only transcript: %d", r.StatusCode)
	}
	if r := do(t, nil, http.MethodGet, srv.URL+"/export/pin/txt", adminTok, ""); r.StatusCode != http.StatusOK {
		t.Errorf("admin export: %d", r.StatusCode)
	}

	// The QR's join code admits without the PIN; an old code does not.
	scanner, _ := cookiejar.New(nil)
	phone := &http.Client{Jar: scanner, CheckRedirect: reader.CheckRedirect}
	code := site.access.joinCode("pin", 2)
	r := do(t, phone, http.MethodGet, srv.URL+"/j/"+code+"?lang=en", "", "")
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/r/pin?lang=en" {
		t.Fatalf("join: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if r := do(t, phone, http.MethodGet, srv.URL+"/api/rooms/pin/talks", "", ""); r.StatusCode != http.StatusOK {
		t.Errorf("after scanning: %d", r.StatusCode)
	}
	if r := do(t, phone, http.MethodGet, srv.URL+"/j/"+site.access.joinCode("pin", 1), "", ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("a revoked code: %d", r.StatusCode)
	}
	if strings.Contains(code, "pin") || len(code) < 10 {
		t.Errorf("join code %q is guessable", code)
	}
}

func TestExportsOff(t *testing.T) {
	_, srv := secureServer(t)
	if r := do(t, nil, http.MethodGet, srv.URL+"/export/off/txt", adminTok, ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("exports off, admin: %d", r.StatusCode)
	}
	if r := do(t, nil, http.MethodGet, srv.URL+"/export/open/txt", "", ""); r.StatusCode != http.StatusOK {
		t.Errorf("open event export: %d", r.StatusCode)
	}
}

func TestSecurityHeaders(t *testing.T) {
	_, srv := secureServer(t)
	for path, framed := range map[string]bool{"/r/open": true, "/admin": false, "/operator/open": false, "/bank": false} {
		resp, _ := get(t, srv.URL+path)
		h := resp.Header
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" ||
			!strings.Contains(h.Get("Content-Security-Policy"), "default-src 'self'") {
			t.Errorf("%s: missing headers %v", path, h)
		}
		if got := h.Get("X-Frame-Options") == ""; got != framed {
			t.Errorf("%s: may be framed = %v, want %v", path, got, framed)
		}
	}
}

func TestViewerCaps(t *testing.T) {
	site, srv := secureServer(t)
	site.MaxViewersPerIP = 1
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events/open", nil)
	first, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Body.Close()
	if r := do(t, nil, http.MethodGet, srv.URL+"/events/open", "", ""); r.StatusCode != http.StatusServiceUnavailable || r.Header.Get("Retry-After") == "" {
		t.Errorf("second connection from the address: %d", r.StatusCode)
	}
}

func TestIngestTokenAsSubprotocolAndRevocationCutsIt(t *testing.T) {
	_, srv := secureServer(t)
	tok := mint(t, srv, "open")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ingest/open"
	if _, _, err := websocket.Dial(ctx, ws, &websocket.DialOptions{Subprotocols: []string{"dubash", "dubash-token.wrong"}}); err == nil {
		t.Fatal("a wrong token connected")
	}
	c, _, err := websocket.Dial(ctx, ws, &websocket.DialOptions{Subprotocols: []string{"dubash", "dubash-token." + tok}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if c.Subprotocol() != "dubash" {
		t.Errorf("subprotocol %q", c.Subprotocol())
	}
	c.Write(ctx, websocket.MessageBinary, make([]byte, 3200))
	waitFor(t, func() bool { return liveStatus(t, srv, "open") })
	do(t, nil, http.MethodPost, srv.URL+"/api/admin/rooms/open/publisher-tokens/revoke", adminTok, "")
	_, _, err = c.Read(ctx)
	if websocket.CloseStatus(err) != StatusRevoked {
		t.Errorf("after revoking: %v, want close %d", err, StatusRevoked)
	}
}

func liveStatus(t *testing.T, srv *httptest.Server, id string) bool {
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/admin/status", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sts []room.Status
	json.NewDecoder(resp.Body).Decode(&sts)
	for _, s := range sts {
		if s.ID == id {
			return s.Live
		}
	}
	return false
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for range 100 {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestRetentionAndDelete(t *testing.T) {
	site, srv := secureServer(t)
	ctx := context.Background()
	now := time.Now()
	for _, seg := range []caption.Segment{
		{ID: 1, Room: "off", Text: "old", At: now.AddDate(0, 0, -40)},
		{ID: 2, Room: "off", Text: "new", At: now},
		{ID: 1, Room: "open", Text: "kept: no retention", At: now.AddDate(0, 0, -400)},
	} {
		site.store.Append(seg)
	}
	site.Bank.SaveCorrection(ctx, bank.Correction{Event: "off", Segment: 1, Raw: "a", Corrected: "b", At: now.AddDate(0, 0, -40)})
	site.retainOnce(ctx, now)
	if segs, _, _ := site.store.Load("off"); len(segs) != 1 || segs[0].Text != "new" {
		t.Errorf("after retention: %+v", segs)
	}
	if segs, _, _ := site.store.Load("open"); len(segs) != 1 {
		t.Errorf("a room without retention lost lines: %+v", segs)
	}
	if cs, _ := site.Bank.Corrections(ctx, "off"); len(cs) != 0 {
		t.Errorf("old live fixes kept: %+v", cs)
	}

	if r := do(t, nil, http.MethodDelete, srv.URL+"/api/admin/rooms/open/transcript", adminTok, ""); r.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if segs, _, _ := site.store.Load("open"); segs != nil {
		t.Errorf("after delete: %+v", segs)
	}
}
