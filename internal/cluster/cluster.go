// Copyright 2026 the Dubash contributors. Apache-2.0.

package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/UtsavInTech/dubash/internal/config"
)

const leaseTTL = 30 * time.Second
const renewScript = `if redis.call('get',KEYS[1]) == ARGV[1] then return redis.call('pexpire',KEYS[1],ARGV[2]) else return 0 end`
const releaseScript = `if redis.call('get',KEYS[1]) == ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`

// Coordinator holds leases for a fixed room shard. The process is cancelled
// immediately on a renewal error, before the 30-second leases can expire.
// Room shards are explicit: a failed owner is restarted, never silently hashed
// onto a server with stale in-memory transcript state.
type Coordinator struct {
	Redis          Redis
	URL, Namespace string
	Rooms          []config.Room
	Owned          []config.Room
	value          string
	ready          atomic.Bool
	cancel         context.CancelFunc
	stopped        chan struct{}
}

// Start acquires every shard lease before transcripts are restored. This is
// important: a standby must not load stale segment IDs while an old owner runs.
func Start(ctx context.Context, redisURL, nodeURL, namespace, roomIDs string, cfg config.Config) (*Coordinator, context.Context, error) {
	u, err := url.Parse(nodeURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, ctx, errors.New("NODE_URL must be this node's internal http(s) origin")
	}
	if namespace == "" {
		namespace = "dubash"
	}
	ids := strings.Split(roomIDs, ",")
	seen := map[string]bool{}
	for i := range ids {
		ids[i] = strings.TrimSpace(ids[i])
		if ids[i] == "" || seen[ids[i]] {
			return nil, ctx, errors.New("ROOM_IDS must list distinct nonempty room IDs")
		}
		seen[ids[i]] = true
	}
	c := &Coordinator{Redis: Redis{URL: redisURL}, URL: strings.TrimRight(nodeURL, "/"), Namespace: namespace, Rooms: cfg.Rooms, stopped: make(chan struct{})}
	for _, r := range cfg.Rooms {
		if slices.Contains(ids, r.ID) {
			c.Owned = append(c.Owned, r)
			delete(seen, r.ID)
		}
	}
	if len(seen) != 0 {
		return nil, ctx, errors.New("ROOM_IDS contains unknown rooms")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, ctx, err
	}
	c.value = hex.EncodeToString(nonce) + "|" + c.URL
	claimCtx, claimCancel := context.WithTimeout(ctx, 10*time.Second)
	defer claimCancel()
	for i, r := range c.Owned {
		result, err := c.Redis.Do(claimCtx, "SET", c.key(r.ID), c.value, "NX", "PX", fmt.Sprint(leaseTTL.Milliseconds()))
		if err != nil || result != "OK" {
			for _, acquired := range c.Owned[:i] {
				c.Redis.Do(context.WithoutCancel(ctx), "EVAL", releaseScript, "1", c.key(acquired.ID), c.value)
			}
			return nil, ctx, fmt.Errorf("room %s: ownership unavailable (%v)", r.ID, err)
		}
	}
	nodeCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.ready.Store(true)
	go func() {
		defer close(c.stopped)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-nodeCtx.Done():
				c.ready.Store(false)
				return
			case <-ticker.C:
				// One deadline for the complete heartbeat, even with many rooms.
				hctx, done := context.WithTimeout(nodeCtx, 4*time.Second)
				failed := false
				for _, r := range c.Owned {
					result, err := c.Redis.Do(hctx, "EVAL", renewScript, "1", c.key(r.ID), c.value, fmt.Sprint(leaseTTL.Milliseconds()))
					if err != nil || result != "1" {
						failed = true
						break
					}
				}
				done()
				if failed {
					c.ready.Store(false)
					cancel()
					return
				}
			}
		}
	}()
	return c, nodeCtx, nil
}

func (c *Coordinator) key(id string) string { return c.Namespace + ":owner:" + id }

// Close must run after all room pipelines have drained. Leases are never
// released early during shutdown, including hijacked microphone WebSockets.
func (c *Coordinator) Close() {
	c.ready.Store(false)
	c.cancel()
	<-c.stopped
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for _, r := range c.Owned {
		c.Redis.Do(ctx, "EVAL", releaseScript, "1", c.key(r.ID), c.value)
	}
}

func (c *Coordinator) owner(ctx context.Context, id string) (string, error) {
	value, err := c.Redis.Do(ctx, "GET", c.key(id))
	if err != nil {
		return "", err
	}
	_, owner, ok := strings.Cut(value, "|")
	if !ok || owner == "" {
		return "", errors.New("room owner is unavailable")
	}
	return owner, nil
}

// Handler preserves SSE flushing, Last-Event-ID and WebSocket upgrades through
// Go's reverse proxy. No client-IP stickiness is required at the load balancer.
func (c *Coordinator) Handler(local http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			local.ServeHTTP(w, r)
			return
		}
		if !c.ready.Load() {
			http.Error(w, "node is draining", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/readyz" {
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			if _, err := c.Redis.Do(ctx, "PING"); err != nil {
				http.Error(w, "coordination unavailable", 503)
				return
			}
			local.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && (r.URL.Path == "/api/rooms" || r.URL.Path == "/api/admin/status") && r.Header.Get("X-Dubash-Catalog") != "local" {
			c.catalog(w, r)
			return
		}
		id := roomID(r.URL.Path)
		if id == "" {
			local.ServeHTTP(w, r)
			return
		}
		known := false
		for _, rc := range c.Rooms {
			if rc.ID == id {
				known = true
				break
			}
		}
		if !known {
			local.ServeHTTP(w, r)
			return
		}
		for _, rc := range c.Owned {
			if rc.ID == id {
				local.ServeHTTP(w, r)
				return
			}
		}
		owner, err := c.owner(r.Context(), id)
		if err != nil || owner == c.URL {
			http.Error(w, "room owner unavailable", 503)
			return
		}
		target, err := url.Parse(owner)
		if err != nil {
			http.Error(w, "invalid room owner", 503)
			return
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.FlushInterval = -1
		proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) { http.Error(w, "room owner unavailable", 503) }
		proxy.ServeHTTP(w, r)
	})
}

func roomID(path string) string {
	p := strings.Split(strings.Trim(path, "/"), "/")
	if len(p) >= 2 && slices.Contains([]string{"r", "operator", "events", "ingest", "export", "qr"}, p[0]) {
		return p[1]
	}
	if len(p) >= 3 && p[0] == "api" && p[1] == "rooms" {
		return p[2]
	}
	if len(p) >= 4 && p[0] == "api" && p[1] == "admin" && (p[2] == "rooms" || p[2] == "events") {
		return p[3]
	}
	return ""
}

func (c *Coordinator) catalog(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	owners := map[string]bool{}
	for _, rc := range c.Rooms {
		owner, err := c.owner(ctx, rc.ID)
		if err != nil {
			http.Error(w, "room catalog temporarily unavailable", 503)
			return
		}
		owners[owner] = true
	}
	var entries []json.RawMessage
	client := &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	for owner := range owners {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, owner+r.URL.RequestURI(), nil)
		if err != nil {
			http.Error(w, "invalid owner", 503)
			return
		}
		req.Header = r.Header.Clone()
		req.Header.Set("X-Dubash-Catalog", "local")
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "room owner unavailable", 503)
			return
		}
		var batch []json.RawMessage
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			http.Error(w, http.StatusText(resp.StatusCode), resp.StatusCode)
			return
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&batch)
		resp.Body.Close()
		if err != nil {
			http.Error(w, "invalid room catalog", 503)
			return
		}
		entries = append(entries, batch...)
	}
	byID := map[string]json.RawMessage{}
	for _, entry := range entries {
		var identity struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(entry, &identity); err != nil {
			http.Error(w, "invalid room identity", 503)
			return
		}
		byID[identity.ID] = entry
	}
	ordered := make([]json.RawMessage, 0, len(c.Rooms))
	for _, rc := range c.Rooms {
		if entry, ok := byID[rc.ID]; ok {
			ordered = append(ordered, entry)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ordered)
}
