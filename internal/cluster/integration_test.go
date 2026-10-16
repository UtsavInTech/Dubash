// Copyright 2026 the Dubash contributors. Apache-2.0.

package cluster

import (
	"context"
	"fmt"
	"github.com/coder/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/config"
)

func TestRedisOwnershipAndCrossNodeSSE(t *testing.T) {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("set TEST_REDIS_URL for real Redis integration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Config{Rooms: []config.Room{{ID: "a", Source: "en"}, {ID: "b", Source: "hi"}}}
	ns := fmt.Sprintf("test:%d", time.Now().UnixNano())
	// Each server is already listening before its URL is registered as an owner.
	var handlerA, handlerB http.Handler
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handlerA.ServeHTTP(w, r) }))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handlerB.ServeHTTP(w, r) }))
	defer b.Close()
	ca, _, err := Start(ctx, redisURL, a.URL, ns, "a", cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ca.Close()
	cb, _, err := Start(ctx, redisURL, b.URL, ns, "b", cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer cb.Close()
	local := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/ingest/b" {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				kind, data, err := conn.Read(r.Context())
				if err == nil {
					conn.Write(r.Context(), kind, data)
				}
				return
			}
			if r.URL.Path == "/api/rooms" {
				fmt.Fprintf(w, `[{"id":%q}]`, name)
				return
			}
			if r.Header.Get("Last-Event-ID") != "instance-12" {
				http.Error(w, "resume header lost", 400)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "id: instance-13\nevent: final\ndata: %s\n\n", name)
		})
	}
	handlerA, handlerB = ca.Handler(local("a")), cb.Handler(local("b"))
	if dup, _, err := Start(ctx, redisURL, "http://duplicate:8080", ns, "a", cfg); err == nil {
		dup.Close()
		t.Fatal("duplicate room owner accepted")
	}
	req, _ := http.NewRequest("GET", a.URL+"/events/b", nil)
	req.Header.Set("Last-Event-ID", "instance-12")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(data) != "id: instance-13\nevent: final\ndata: b\n\n" {
		t.Fatalf("cross-node replay: %d %s", resp.StatusCode, data)
	}
	resp, err = http.Get(a.URL + "/api/rooms")
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(data) < 20 {
		t.Fatalf("aggregate catalog: %d %s", resp.StatusCode, data)
	}
	socketCtx, socketCancel := context.WithTimeout(ctx, time.Second)
	defer socketCancel()
	conn, _, err := websocket.Dial(socketCtx, a.URL+"/ingest/b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.Write(socketCtx, websocket.MessageBinary, []byte{1, 2, 3, 4}); err != nil {
		conn.CloseNow()
		t.Fatal(err)
	}
	kind, echoed, err := conn.Read(socketCtx)
	conn.CloseNow()
	if err != nil || kind != websocket.MessageBinary || string(echoed) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("cross-node microphone: %v %v %v", kind, echoed, err)
	}
	// Losing ownership must cancel the owner and reject new traffic.
	if _, err = ca.Redis.Do(ctx, "DEL", ca.key("a")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(7 * time.Second)
	for ca.ready.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if ca.ready.Load() {
		t.Fatal("lease loss did not fence node")
	}
	rr := httptest.NewRecorder()
	handlerA.ServeHTTP(rr, httptest.NewRequest("GET", "/readyz", nil))
	if rr.Code != 503 {
		t.Fatalf("lost owner remained ready: %d", rr.Code)
	}
}
