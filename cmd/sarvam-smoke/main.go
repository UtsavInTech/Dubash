// Copyright 2026 the Dubash contributors. Apache-2.0.

// Command sarvam-smoke streams PCM16 mono 16 kHz from stdin to Sarvam's
// realtime speech-to-text WebSocket at playback speed and prints every
// raw server message with its arrival time, to check the wire format and
// latency from the terminal:
//
//	afconvert -f WAVE -d LEI16@16000 -c 1 talk.m4a talk.wav
//	tail -c +45 talk.wav | go run ./cmd/sarvam-smoke -lang hi-IN -mode codemix
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func main() {
	lang := flag.String("lang", "hi-IN", "language_code")
	model := flag.String("model", "saaras:v4", "model")
	mode := flag.String("mode", "", "mode: transcribe, codemix, verbatim, translit, translate")
	keyterms := flag.String("keyterms", "", "JSON array of keyterms (saaras:v4 only)")
	stream := flag.String("stream", "", "stream_type: fast, balanced, simulated")
	extra := flag.String("q", "", "more query parameters, e.g. silence_duration_ms=500&threshold=0.4")
	partials := flag.Bool("partials", true, "print transcript.partial messages")
	flag.Parse()
	key := os.Getenv("SARVAM_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "SARVAM_API_KEY is not set")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	q := url.Values{"language_code": {*lang}, "model": {*model}, "encoding": {"linear16"}, "sample_rate": {"16000"}}
	for k, v := range map[string]string{"mode": *mode, "keyterms": *keyterms, "stream_type": *stream} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if more, err := url.ParseQuery(*extra); err == nil {
		for k, v := range more {
			q[k] = v
		}
	}
	c, resp, err := websocket.Dial(ctx, "wss://api.sarvam.ai/speech-to-text-realtime/ws?"+q.Encode(), &websocket.DialOptions{
		HTTPHeader: http.Header{"Api-Subscription-Key": {key}},
	})
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			fmt.Fprintf(os.Stderr, "dial: %v (%s %s)\n", err, resp.Status, body)
		} else {
			fmt.Fprintln(os.Stderr, "dial:", err)
		}
		os.Exit(1)
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	start := time.Now()
	done := make(chan struct{})

	go func() {
		br := bufio.NewReader(os.Stdin)
		buf := make([]byte, 3200) // 100 ms
		var sent time.Duration
		for {
			n, err := io.ReadFull(br, buf)
			if n > 0 {
				sent += time.Duration(n) * time.Second / 32000
				time.Sleep(time.Until(start.Add(sent)))
				msg, _ := json.Marshal(map[string]string{"event": "audio_input", "audio": base64.StdEncoding.EncodeToString(buf[:n])})
				if c.Write(ctx, websocket.MessageText, msg) != nil {
					return
				}
			}
			if err != nil {
				break
			}
		}
		fmt.Printf("%7.2fs  >> audio done (%.1f s); sending end\n", time.Since(start).Seconds(), sent.Seconds())
		c.Write(ctx, websocket.MessageText, []byte(`{"event":"end"}`))
		close(done)
	}()

	// The server does not always close after "end": stop once it has been
	// quiet for a few seconds after the audio ran out.
	quiet := time.NewTimer(time.Hour)
	go func() {
		<-done
		quiet.Reset(4 * time.Second)
		<-quiet.C
		c.Close(websocket.StatusNormalClosure, "")
	}()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			fmt.Printf("%7.2fs  closed: %v\n", time.Since(start).Seconds(), err)
			return
		}
		if !*partials && strings.Contains(string(data), `"transcript.partial"`) {
			continue
		}
		fmt.Printf("%7.2fs  %s\n", time.Since(start).Seconds(), data)
		select {
		case <-done:
			quiet.Reset(4 * time.Second)
		default:
		}
	}
}
