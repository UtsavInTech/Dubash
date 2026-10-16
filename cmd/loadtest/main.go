// Copyright 2026 the Dubash contributors. Apache-2.0.

// Command loadtest exercises real SSE connections and microphone WebSockets.
// Run only against a -fake server; deploy/loadtest.sh starts one with API keys cleared.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type report struct {
	Viewers           int     `json:"viewers"`
	Connections       int     `json:"connections"`
	Finals            int     `json:"finals"`
	Translations      int     `json:"translations"`
	Errors            int     `json:"errors"`
	FirstCaptionP95MS int64   `json:"first_caption_p95_ms"`
	DurationSeconds   float64 `json:"duration_seconds"`
}

func main() {
	base := flag.String("url", "http://127.0.0.1:8080", "fake server URL")
	room := flag.String("room", "main-hall", "room ID")
	lang := flag.String("lang", "en", "translation language")
	n := flag.Int("viewers", 200, "concurrent viewers")
	duration := flag.Duration("duration", 15*time.Second, "PCM streaming duration (at least 6s)")
	flag.Parse()
	if *n < 1 || *duration < 6*time.Second {
		fmt.Fprintln(os.Stderr, "viewers must be positive and duration at least 6s")
		os.Exit(2)
	}
	result, err := run(strings.TrimRight(*base, "/"), *room, *lang, *n, *duration)
	json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(base, room, lang string, n int, duration time.Duration) (report, error) {
	start := time.Now()
	result := report{Viewers: n}
	var mu sync.Mutex
	var first []int64
	ctx, cancel := context.WithTimeout(context.Background(), duration+15*time.Second)
	defer cancel()
	var ready, finished sync.WaitGroup
	ready.Add(n)
	finished.Add(n)
	for range n {
		go func() {
			defer finished.Done()
			req, err := http.NewRequestWithContext(ctx, "GET", base+"/events/"+url.PathEscape(room)+"?lang="+url.QueryEscape(lang), nil)
			if err != nil {
				mu.Lock()
				result.Errors++
				mu.Unlock()
				ready.Done()
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				mu.Lock()
				result.Errors++
				mu.Unlock()
				ready.Done()
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				mu.Lock()
				result.Errors++
				mu.Unlock()
				ready.Done()
				return
			}
			mu.Lock()
			result.Connections++
			mu.Unlock()
			ready.Done()
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 4096), 1<<20)
			seen := false
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "event: ") {
					mu.Lock()
					switch strings.TrimPrefix(line, "event: ") {
					case "final":
						result.Finals++
						if !seen {
							first = append(first, time.Since(start).Milliseconds())
							seen = true
						}
					case "translation":
						result.Translations++
					}
					mu.Unlock()
				}
			}
			mu.Lock()
			if !seen {
				result.Errors++
			}
			mu.Unlock()
		}()
	}
	ready.Wait()
	// ADMIN_TOKEN is supported by the existing microphone API. Never print it.
	ingest := base + "/ingest/" + url.PathEscape(room)
	headers := http.Header{}
	if token := os.Getenv("ADMIN_TOKEN"); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	conn, resp, err := websocket.Dial(ctx, ingest, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		cancel()
		finished.Wait()
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return result, fmt.Errorf("microphone connection failed (status %d)", status)
	}
	// A reader handles control frames while audio is streamed.
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	timer := time.NewTimer(duration)
	pcm := make([]byte, 3200)
	// Low-level deterministic tone, rather than silence, also exercises meters.
	for i := 0; i < len(pcm); i += 2 {
		pcm[i], pcm[i+1] = 0, 8
	}
	streaming := true
	for streaming {
		select {
		case <-ticker.C:
			if err = conn.Write(ctx, websocket.MessageBinary, pcm); err != nil {
				streaming = false
			}
		case <-timer.C:
			streaming = false
		case <-ctx.Done():
			err = ctx.Err()
			streaming = false
		}
	}
	ticker.Stop()
	timer.Stop()
	conn.CloseNow()
	// Allow final translations to drain before ending the audience streams.
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
	}
	cancel()
	finished.Wait()
	mu.Lock()
	sort.Slice(first, func(i, j int) bool { return first[i] < first[j] })
	if len(first) > 0 {
		result.FirstCaptionP95MS = first[(len(first)-1)*95/100]
	}
	result.DurationSeconds = time.Since(start).Seconds()
	mu.Unlock()
	if err != nil || result.Errors > 0 || result.Finals < n || result.Translations < n {
		return result, fmt.Errorf("load test failed: check connections, captions and translations in the report")
	}
	return result, nil
}
