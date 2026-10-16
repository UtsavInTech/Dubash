package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrganiserSessionAndPublicPages(t *testing.T) {
	s := managementServer(t)
	h := s.Handler()
	for _, path := range []string{"/", "/login", "/events"} {
		out := managementRequest(h, "GET", path, "", "")
		if out.Code != 200 {
			t.Fatalf("public page %s: %d", path, out.Code)
		}
	}
	req := httptest.NewRequest("POST", "https://captions.example.org/api/session", strings.NewReader(`{"key":"wrong"}`))
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 401 {
		t.Fatal("wrong key accepted")
	}
	req = httptest.NewRequest("POST", "https://captions.example.org/api/session", strings.NewReader(`{"key":"admin-ui-test"}`))
	req.Header.Set("Origin", "https://evil.example")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatal("cross-site login accepted")
	}
	req = httptest.NewRequest("POST", "https://captions.example.org/api/session", strings.NewReader(`{"key":"admin-ui-test"}`))
	req.Header.Set("Origin", "https://captions.example.org")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 204 {
		t.Fatalf("login: %d %s", out.Code, out.Body)
	}
	cookies := out.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("insecure session cookie: %+v", cookies)
	}
	session := cookies[0]
	req = httptest.NewRequest("GET", "/api/admin/status", nil)
	req.AddCookie(session)
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatalf("session did not authenticate: %d", out.Code)
	}
	req = httptest.NewRequest("GET", "/api/admin/status", nil)
	forged := *session
	forged.Value += "tampered"
	req.AddCookie(&forged)
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 401 {
		t.Fatal("forged session accepted")
	}
	req = httptest.NewRequest("DELETE", "https://captions.example.org/api/session", nil)
	req.AddCookie(session)
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 204 || out.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not expire the session")
	}
	req = httptest.NewRequest("POST", "https://captions.example.org/api/admin/events", strings.NewReader(`{"id":"cross-site","title":"Cross-site","source":"hi"}`))
	req.AddCookie(session)
	req.Header.Set("Origin", "https://evil.example")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 403 {
		t.Fatal("cross-site cookie-authenticated mutation accepted")
	}
	// Catalyst accepts custom application headers without intercepting a Bearer token.
	req = httptest.NewRequest("GET", "/api/admin/status", nil)
	req.Header.Set("X-Dubash-Token", "admin-ui-test")
	out = httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatal("custom header rejected")
	}
}
