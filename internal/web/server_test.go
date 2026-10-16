package web

import (
	"path/filepath"

	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/UtsavInTech/dubash/internal/bank"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"rsc.io/qr"

	"github.com/UtsavInTech/dubash/internal/asr"
	"github.com/UtsavInTech/dubash/internal/caption"
	"github.com/UtsavInTech/dubash/internal/config"
	"github.com/UtsavInTech/dubash/internal/export"
	"github.com/UtsavInTech/dubash/internal/room"
	"github.com/UtsavInTech/dubash/internal/store"
	"github.com/UtsavInTech/dubash/internal/speaker"
	"github.com/UtsavInTech/dubash/internal/translate"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	var rooms []*room.Room
	for _, rc := range []config.Room{
		// sala-a translates whether or not anyone reads it, so exports are
		// complete; translating only languages being read is tested in
		// internal/room.
		{ID: "sala-a", Title: "A", Source: "en", Targets: []string{"es"}, AlwaysTranslate: []string{"en", "es"}},
		{ID: "sala-b", Title: "B", Source: "es", Targets: []string{"en"}},
	} {
		rm, err := room.New(rc, room.Deps{ASR: asr.Fake{Every: 1}, Translator: translate.Fake{}, Store: st, Log: log})
		if err != nil {
			t.Fatal(err)
		}
		rooms = append(rooms, rm)
	}
	site := New(rooms, st, "secret", log)
	if site.Bank, err = bank.OpenFile(filepath.Join(t.TempDir(), "bank.json")); err != nil {
		t.Fatal(err)
	}
	if site.Speakers, err = speaker.OpenFile(filepath.Join(t.TempDir(), "speakers.json")); err != nil { t.Fatal(err) }
	site.SpeakerDir = t.TempDir()
	srv := httptest.NewServer(site.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string, header ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestListRooms(t *testing.T) {
	srv := newTestServer(t)
	resp, body := get(t, srv.URL+"/api/rooms")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var rooms []roomInfo
	if err := json.Unmarshal([]byte(body), &rooms); err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 2 || rooms[0].ID != "sala-a" || rooms[1].Source != "es" || rooms[0].Live {
		t.Fatalf("rooms = %+v", rooms)
	}
}

func TestAuth(t *testing.T) {
	srv := newTestServer(t)
	if resp, _ := get(t, srv.URL+"/ingest/sala-a"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ingest without token: %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/api/admin/status"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("admin without token: %d", resp.StatusCode)
	}
	resp, body := get(t, srv.URL+"/api/admin/status", "Authorization", "Bearer secret")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"id":"sala-b"`) {
		t.Fatalf("admin with token: %d %s", resp.StatusCode, body)
	}
}

func TestPagesAndNotFound(t *testing.T) {
	srv := newTestServer(t)
	for _, p := range []string{"/", "/r/sala-a", "/operator/sala-a", "/admin", "/posters"} {
		resp, _ := get(t, srv.URL+p)
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			t.Errorf("%s: %d %s", p, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
	if resp, _ := get(t, srv.URL+"/events/nope"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown room: %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/export/sala-a/pdf"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown format: %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/samples/missing.wav"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing sample: %d", resp.StatusCode)
	}
}

// The service worker keeps the overlay loadable while the server is down,
// so it must be served from the root (its scope) and never HTTP-cached.
func TestServiceWorkerIsServedFromTheRoot(t *testing.T) {
	srv := newTestServer(t)
	resp, body := get(t, srv.URL+"/sw.js")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("/sw.js: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", resp.Header.Get("Cache-Control"))
	}
	if !strings.Contains(body, "addEventListener('fetch'") {
		t.Error("/sw.js does not handle fetches")
	}
}

func TestQRCodePointsAtTheRoom(t *testing.T) {
	srv := newTestServer(t)
	resp, svg := get(t, srv.URL+"/qr/sala-a?lang=es")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(svg, "<svg") {
		t.Fatalf("qr: %d %s %.40q", resp.StatusCode, resp.Header.Get("Content-Type"), svg)
	}
	if resp, _ := get(t, srv.URL+"/qr/nope"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown room: %d", resp.StatusCode)
	}

	_, body := get(t, srv.URL+"/api/site", "X-Forwarded-Proto", "https")
	var site struct{ URL string }
	if err := json.Unmarshal([]byte(body), &site); err != nil {
		t.Fatal(err)
	}
	if want := "https://" + strings.TrimPrefix(srv.URL, "http://"); site.URL != want {
		t.Errorf("site url = %q, want %q", site.URL, want)
	}
}

func TestQRSVGDrawsEveryDarkModule(t *testing.T) {
	code, err := qr.Encode("https://example.org/r/sala-a", qr.M)
	if err != nil {
		t.Fatal(err)
	}
	dark := 0
	for y := range code.Size {
		for x := range code.Size {
			if code.Black(x, y) {
				dark++
			}
		}
	}
	drawn := 0
	for _, run := range regexp.MustCompile(`h(\d+)v1`).FindAllStringSubmatch(qrSVG(code), -1) {
		n, _ := strconv.Atoi(run[1])
		drawn += n
	}
	if drawn != dark {
		t.Fatalf("drew %d modules, code has %d", drawn, dark)
	}
}

func TestLiveFlowAndExport(t *testing.T) {
	srv := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A Spanish reader: the server translates into Spanish because of it.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events/sala-a?lang=es", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	translations := make(chan string, 8)
	ids := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		event := ""
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				ids <- strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && event == "translation":
				translations <- strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if err := ws.Write(ctx, websocket.MessageBinary, make([]byte, asr.BytesPerSecond/10)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case data := <-translations:
		var tr caption.Translation
		if err := json.Unmarshal([]byte(data), &tr); err != nil {
			t.Fatal(err)
		}
		if tr.Lang != "es" || tr.Segment != 0 || tr.Text != "[es] Welcome to Nerdearla." {
			t.Fatalf("translation = %+v", tr)
		}
	case <-ctx.Done():
		t.Fatal("no translation received")
	}
	if len(ids) < 2 { // the final and its translation
		t.Fatalf("committed messages carry no SSE ids")
	}
	ws.Close(websocket.StatusNormalClosure, "")

	_, vtt := get(t, srv.URL+"/export/sala-a/vtt?lang=es")
	if !strings.HasPrefix(vtt, "WEBVTT") || !strings.Contains(vtt, "00:00:00.000 --> 00:00:01.000\n[es] Welcome to Nerdearla.") {
		t.Fatalf("vtt = %q", vtt)
	}
}

func TestIngestTakesTheSpokenLanguage(t *testing.T) {
	srv := newTestServer(t)
	if resp, _ := get(t, srv.URL+"/ingest/sala-a?token=secret&lang=pt"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("pt in an en/es room: %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a?token=secret&lang=es", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if err := ws.Write(ctx, websocket.MessageBinary, make([]byte, asr.BytesPerSecond/10)); err != nil {
			t.Fatal(err)
		}
	}
	ws.Close(websocket.StatusNormalClosure, "")
	for {
		_, body := get(t, srv.URL+"/export/sala-a/txt?lang=es")
		if body == "Bienvenidos a Nerdearla.\n" {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("es transcript = %q", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, body := get(t, srv.URL+"/export/sala-a/txt?lang=en"); body != "[en] Bienvenidos a Nerdearla.\n" {
		t.Fatalf("en transcript = %q", body)
	}
	_, body := get(t, srv.URL+"/api/rooms")
	if !strings.Contains(body, `"id":"sala-a","title":"A","source":"es","targets":["en"]`) {
		t.Fatalf("rooms = %s", body)
	}
}

func TestEventsMarkTheEndOfTheReplay(t *testing.T) {
	srv := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events/sala-a", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	var events []string
	for sc.Scan() {
		if e, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			events = append(events, e)
			if e == "caught-up" {
				break
			}
		}
	}
	if len(events) != 2 || events[0] != "status" || events[1] != "caught-up" {
		t.Fatalf("events = %v", events)
	}
}

func TestExportRejectsUnknownLang(t *testing.T) {
	srv := newTestServer(t)
	for _, lang := range []string{"en", "es", ""} {
		if resp, _ := get(t, srv.URL+"/export/sala-a/txt?lang="+lang); resp.StatusCode != http.StatusOK {
			t.Errorf("lang %q: %d", lang, resp.StatusCode)
		}
	}
	for _, lang := range []string{"pt", "fr", `x"; y`} {
		if resp, _ := get(t, srv.URL+"/export/sala-a/txt?lang="+url.QueryEscape(lang)); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("lang %q: %d, want 400", lang, resp.StatusCode)
		}
	}
}

// stuckEngine never reads audio and only returns when its context ends,
// like an engine wedged in a blocking reconnect.
type stuckEngine struct{}

func (stuckEngine) Run(ctx context.Context, _ asr.Config, _ <-chan []byte, _ chan<- asr.Event) error {
	<-ctx.Done()
	return nil
}

func TestIngestEndsWhenOperatorLeavesStuckEngine(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	rm, err := room.New(config.Room{ID: "sala-a", Source: "en", Targets: []string{}},
		room.Deps{ASR: stuckEngine{}, Translator: translate.Fake{}, Store: st, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	s := New([]*room.Room{rm}, st, "", log)
	s.drainGrace = 200 * time.Millisecond
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 80 { // well past the 50-chunk audio buffer
		if err := ws.Write(ctx, websocket.MessageBinary, make([]byte, asr.BytesPerSecond/10)); err != nil {
			t.Fatal(err)
		}
	}
	if !rm.Status().Live {
		t.Fatal("room should be live while the operator streams")
	}
	ws.CloseNow()
	deadline := time.Now().Add(2 * time.Second)
	for rm.Status().Live {
		if time.Now().After(deadline) {
			t.Fatal("room still live 2 s after the operator disconnected")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// failingEngine reports an unrecoverable error straight away.
type failingEngine struct{}

func (failingEngine) Run(context.Context, asr.Config, <-chan []byte, chan<- asr.Event) error {
	return errors.New("connect: API key not valid")
}

func TestIngestReportsFatalEngineError(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	rm, err := room.New(config.Room{ID: "sala-a", Source: "en", Targets: []string{}},
		room.Deps{ASR: failingEngine{}, Translator: translate.Fake{}, Store: st, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New([]*room.Room{rm}, st, "", log).Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	_, _, err = ws.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusInternalError {
		t.Fatalf("read err = %v, want close 1011", err)
	}
	var ce websocket.CloseError
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "API key not valid") {
		t.Fatalf("close reason = %q, want the engine error", ce.Reason)
	}
	if rm.Status().Live {
		t.Fatal("room still live after a fatal engine error")
	}
}

func TestCloseReasonShowsTheCauseWithoutURLs(t *testing.T) {
	long := strings.Repeat("ñ", 100)
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("connect: API key not valid"), "transcripción: connect: API key not valid"},
		{fmt.Errorf("room sala-a: %w", fmt.Errorf("connect: %w", &url.Error{
			Op: "Get", URL: "wss://generativelanguage.googleapis.com/ws/live?key=SECRET", Err: errors.New("dial tcp: i/o timeout"),
		})), "transcripción: dial tcp: i/o timeout"},
		{errors.New("bad handshake at wss://example.com/x?key=SECRET now"), "transcripción: bad handshake at now"},
		{errors.New(long), "transcripción: " + strings.Repeat("ñ", (maxCloseReason-len("transcripción: "))/2)},
	}
	for _, c := range cases {
		got := closeReason(c.err)
		if got != c.want {
			t.Errorf("closeReason(%v) = %q, want %q", c.err, got, c.want)
		}
		if len(got) > maxCloseReason || !utf8.ValidString(got) {
			t.Errorf("closeReason(%v) = %q: too long or split rune", c.err, got)
		}
	}
}

func TestTalksExportAndNewTalk(t *testing.T) {
	srv := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream := func(query string, secs int) {
		t.Helper()
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a?token=secret"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		for range secs * 10 {
			if err := ws.Write(ctx, websocket.MessageBinary, make([]byte, asr.BytesPerSecond/10)); err != nil {
				t.Fatal(err)
			}
		}
		ws.Close(websocket.StatusNormalClosure, "")
		// The room goes idle once the pipeline has published everything.
		for {
			_, body := get(t, srv.URL+"/api/rooms")
			if !strings.Contains(body, `"id":"sala-a","title":"A","source":"en","targets":["es"],"live":true`) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	stream("", 2)
	stream("&talk="+url.QueryEscape("Kubernetes en producción"), 1)

	resp, body := get(t, srv.URL+"/api/rooms/sala-a/talks")
	var talks []export.Talk
	if err := json.Unmarshal([]byte(body), &talks); resp.StatusCode != http.StatusOK || err != nil {
		t.Fatalf("talks: %d %s %v", resp.StatusCode, body, err)
	}
	if len(talks) != 2 || talks[0].N != 1 || talks[0].Segments != 2 ||
		talks[1].N != 2 || talks[1].Title != "Kubernetes en producción" || talks[1].Segments != 1 || talks[1].Start.IsZero() {
		t.Fatalf("talks = %+v", talks)
	}

	// A talk's subtitles start at zero, to play over that talk's video.
	resp, srt := get(t, srv.URL+"/export/sala-a/srt?lang=es&talk=2")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(srt, "1\n00:00:00,000 --> 00:00:01,000\n") ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), `filename="sala-a-talk2-es.srt"`) {
		t.Fatalf("talk 2 srt: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Disposition"), srt)
	}
	if resp, _ := get(t, srv.URL+"/export/sala-a/srt?talk=9"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing talk: %d, want 404", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/export/sala-a/srt?talk=x"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad talk: %d, want 400", resp.StatusCode)
	}

	// Production can split talks without interrupting the audio.
	post := func(auth string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/admin/rooms/sala-a/talks", strings.NewReader("title=Q%26A"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", auth)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if resp := post(""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("new talk without token: %d", resp.StatusCode)
	}
	if resp := post("Bearer secret"); resp.StatusCode != http.StatusOK {
		t.Fatalf("new talk: %d", resp.StatusCode)
	}
	_, status := get(t, srv.URL+"/api/admin/status", "Authorization", "Bearer secret")
	if !strings.Contains(status, `"talk":3,"talkTitle":"Q\u0026A"`) {
		t.Fatalf("status after new talk: %s", status)
	}
	// The new talk has no lines yet: its transcript is empty, not missing.
	if resp, body := get(t, srv.URL+"/export/sala-a/vtt?talk=3"); resp.StatusCode != http.StatusOK || body != "WEBVTT\n\n" {
		t.Fatalf("empty talk: %d %q", resp.StatusCode, body)
	}
}

func TestMetrics(t *testing.T) {
	srv := newTestServer(t)
	if resp, _ := get(t, srv.URL+"/metrics"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("metrics without token: %d", resp.StatusCode)
	}
	resp, body := get(t, srv.URL+"/metrics", "Authorization", "Bearer secret")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics: %d", resp.StatusCode)
	}
	for _, want := range []string{
		"# TYPE dubash_room_live gauge\n",
		`dubash_room_live{room="sala-a"} 0` + "\n",
		`dubash_room_segments_total{room="sala-b"} 0` + "\n",
		`dubash_room_audio_level_dbfs{room="sala-a"} -99` + "\n",
		"# TYPE dubash_room_listeners gauge\n",
		"# TYPE dubash_room_translation_delay_seconds gauge\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q:\n%s", want, body)
		}
	}

	// After a Spanish reader and a line: readers and delays per language.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events/sala-a?lang=es", nil)
	events, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer events.Body.Close()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		ws.Write(ctx, websocket.MessageBinary, make([]byte, asr.BytesPerSecond/10))
	}
	ws.Close(websocket.StatusNormalClosure, "")
	for {
		_, body = get(t, srv.URL+"/metrics", "Authorization", "Bearer secret")
		if strings.Contains(body, `dubash_room_translation_delay_seconds{room="sala-a",lang="es",quantile="0.95"}`) &&
			strings.Contains(body, `dubash_room_listeners{room="sala-a",lang="es"} 1`) {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("per-language metrics missing:\n%s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPauseResumeEnd(t *testing.T) {
	srv := newTestServer(t)
	for _, action := range []string{"pause", "resume", "end"} {
		resp, err := http.Post(srv.URL+"/api/admin/rooms/sala-a/"+action, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s without token: %d", action, resp.StatusCode)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	control := func(action string) map[string]any {
		t.Helper()
		resp, err := http.Post(srv.URL+"/api/admin/rooms/sala-a/"+action+"?token=secret", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var st map[string]any
		json.NewDecoder(resp.Body).Decode(&st)
		return st
	}
	for !(func() bool { _, b := get(t, srv.URL+"/api/rooms"); return strings.Contains(b, `"live":true`) })() {
		time.Sleep(10 * time.Millisecond)
	}
	if st := control("pause"); st["paused"] != true {
		t.Fatalf("pause: %v", st)
	}
	if st := control("resume"); st["paused"] != false {
		t.Fatalf("resume: %v", st)
	}
	if st := control("end"); st["ended"] != true {
		t.Fatalf("end: %v", st)
	}
	// The operator is told the event ended, with a code that says "do not
	// reconnect".
	_, _, err = ws.Read(ctx)
	if websocket.CloseStatus(err) != StatusEventEnded {
		t.Fatalf("operator socket closed with %v", err)
	}
}

func TestEventsResumeFromLastEventID(t *testing.T) {
	srv := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ingest/sala-a?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 20 { // two lines
		ws.Write(ctx, websocket.MessageBinary, make([]byte, asr.BytesPerSecond/10))
	}
	ws.Close(websocket.StatusNormalClosure, "")
	// Wait for both lines and their translations.
	for {
		if _, body := get(t, srv.URL+"/export/sala-a/txt?lang=es"); strings.Count(body, "[es]") == 2 {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("lines not translated")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// replay reads one connection's replay: ids and events until caught-up.
	replay := func(lastID string) (ids, events []string) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events/sala-a?lang=es", nil)
		if lastID != "" {
			req.Header.Set("Last-Event-ID", lastID)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if id, ok := strings.CutPrefix(line, "id: "); ok {
				ids = append(ids, id)
			}
			if e, ok := strings.CutPrefix(line, "event: "); ok {
				if e == "caught-up" {
					return
				}
				events = append(events, e)
			}
		}
		return
	}
	ids, events := replay("")
	if len(ids) != 4 || strings.Join(events, " ") != "status final translation final translation" {
		t.Fatalf("full replay: ids %v events %v", ids, events)
	}
	// A browser that saw the first line and its translation gets the rest.
	ids2, events2 := replay(ids[1])
	if len(ids2) != 2 || ids2[0] != ids[2] || strings.Join(events2, " ") != "status final translation" {
		t.Fatalf("resume: ids %v events %v", ids2, events2)
	}
}
