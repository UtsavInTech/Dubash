package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/translate"
)

func managementServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(nil, st, "admin-ui-test", log)
	s.PublicURL = "https://captions.example.org"
	s.CreateEvent = func(_ context.Context, cfg config.Room) (*room.Room, error) {
		return room.New(cfg, room.Deps{ASR: asr.Fake{}, Translator: translate.Fake{}, Store: st, Log: log})
	}
	return s
}
func managementRequest(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	return out
}
func TestManagementCreateAndShareWithoutSockets(t *testing.T) {
	s := managementServer(t)
	handler := s.Handler()
	body := `{"id":"community-day","title":"Community Day","source":"hi","targets":["en","te"],"audiencePIN":"1234","exports":"admin","retentionDays":30}`
	if out := managementRequest(handler, "POST", "/api/admin/events", body, ""); out.Code != 401 {
		t.Fatalf("unauthenticated create: %d", out.Code)
	}
	out := managementRequest(handler, "POST", "/api/admin/events", body, "admin-ui-test")
	if out.Code != 201 {
		t.Fatalf("create: %d %s", out.Code, out.Body)
	}
	if out = managementRequest(handler, "POST", "/api/admin/events", body, "admin-ui-test"); out.Code != 409 {
		t.Fatalf("duplicate: %d %s", out.Code, out.Body)
	}
	out = managementRequest(handler, "GET", "/api/admin/rooms/community-day/links", "", "admin-ui-test")
	var links map[string]string
	if err := json.Unmarshal(out.Body.Bytes(), &links); err != nil {
		t.Fatal(err)
	}
	if links["audience"] != "https://captions.example.org/r/community-day" || strings.Contains(links["audience"], "token") {
		t.Fatalf("wrong audience link: %+v", links)
	}
	if len(out.Result().Cookies()) != 1 {
		t.Fatal("PIN-protected speaker preview did not get a reader pass")
	}
	out = managementRequest(handler, "GET", "/api/rooms", "", "")
	if strings.Contains(out.Body.String(), "1234") || !strings.Contains(out.Body.String(), "Community Day") {
		t.Fatalf("public event info: %s", out.Body)
	}
	if out = managementRequest(handler, "POST", "/api/admin/events", `{"id":"bad","title":"Bad","source":"hi","targets":["hi"]}`, "admin-ui-test"); out.Code != 400 {
		t.Fatalf("invalid language: %d", out.Code)
	}
	s.CreateEvent = nil
	if out = managementRequest(s.Handler(), "POST", "/api/admin/events", body, "admin-ui-test"); out.Code != 503 {
		t.Fatalf("unsupported creation: %d", out.Code)
	}
}
func TestManagementConcurrentCatalogReads(t *testing.T) {
	s := managementServer(t)
	h := s.Handler()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"id":"event-%d","title":"Event %d","source":"hi","targets":["en"]}`, i, i)
			if out := managementRequest(h, "POST", "/api/admin/events", body, "admin-ui-test"); out.Code != 201 {
				t.Errorf("create: %d %s", out.Code, out.Body)
			}
			for range 3 {
				managementRequest(h, "GET", "/api/rooms", "", "")
				managementRequest(h, "GET", "/api/admin/status", "", "admin-ui-test")
				managementRequest(h, "GET", "/j/invalid", "", "")
			}
		}(i)
	}
	wg.Wait()
	if len(s.roomSnapshot()) != 20 {
		t.Fatal("events missing")
	}
}

func TestAudienceQRDownloadPNG(t *testing.T) {
	s := managementServer(t)
	body := `{"id":"friends-test","title":"Friends test","source":"hi","targets":["en"],"audiencePIN":"1234"}`
	handler := s.Handler()
	if out := managementRequest(handler, "POST", "/api/admin/events", body, "admin-ui-test"); out.Code != 201 {
		t.Fatal(out.Body)
	}
	path := "/qr/friends-test?format=png&download=1"
	if out := managementRequest(handler, "GET", path, "", ""); out.Code != 401 {
		t.Fatalf("PIN QR leaked: %d", out.Code)
	}
	out := managementRequest(handler, "GET", path, "", "admin-ui-test")
	if out.Code != 200 || out.Header().Get("Content-Type") != "image/png" || out.Header().Get("Content-Disposition") != `attachment; filename="friends-test-qr.png"` {
		t.Fatalf("download: %d %+v", out.Code, out.Header())
	}
	image, err := png.DecodeConfig(bytes.NewReader(out.Body.Bytes()))
	if err != nil || image.Width < 400 || image.Width != image.Height {
		t.Fatalf("invalid share image: %+v %v", image, err)
	}
	if out.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("private QR caching changed")
	}
}
