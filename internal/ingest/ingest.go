// Package ingest streams PCM audio from a local source (ffmpeg reading a
// stage's SRT/RTMP/HLS feed, a capture card, a file) into a Dubash room,
// so a stage can be captioned unattended, without an operator browser.
package ingest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/UtsavInTech/dubash/internal/asr"
)

// ChunkBytes is 100 ms of wire audio (PCM16 mono 16 kHz).
const ChunkBytes = asr.BytesPerSecond / 10

// backlog is how much audio waits while the server is unreachable; older
// audio is dropped so captions resume live instead of lagging behind.
const backlog = 50 // chunks: 5 s

var (
	// ErrUnauthorized means the server rejected the token: retrying won't help.
	ErrUnauthorized = errors.New("ingest: token rejected by the server")
	// ErrUnknownRoom means the server has no such room.
	ErrUnknownRoom = errors.New("ingest: unknown room")
	// ErrLanguage means the room does not serve the requested language.
	ErrLanguage = errors.New("ingest: the room does not serve that language")
	// ErrEnded means the event was ended from the panel.
	ErrEnded = errors.New("ingest: the event was ended from the panel")
)

// statusEventEnded is the close code the server uses for an ended event.
const statusEventEnded websocket.StatusCode = 4001

// Options say where to send the audio.
type Options struct {
	Server string // base URL, e.g. https://subs.example.org
	Room   string
	Token  string
	// Lang is the language spoken on stage (en, es, pt, fr, de, it); empty keeps the
	// room's current one.
	Lang string
	// Talk titles the talk; a new title starts a new talk in the room's
	// transcript. Empty continues the current talk.
	Talk string
	// Realtime paces the input at playback speed. Use it for files; live
	// sources already arrive in real time.
	Realtime bool
	Log      *slog.Logger
	// MinBackoff and MaxBackoff bound the wait between reconnects
	// (defaults 1 s and 30 s).
	MinBackoff, MaxBackoff time.Duration
}

// Endpoint is the room's ingest WebSocket URL.
func (o Options) Endpoint() (string, error) {
	u, err := url.Parse(o.Server)
	if err != nil {
		return "", fmt.Errorf("ingest: server url: %w", err)
	}
	switch u.Scheme {
	case "http", "ws":
		u.Scheme = "ws"
	case "https", "wss":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("ingest: server url must be http(s), got %q", o.Server)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/ingest/" + url.PathEscape(o.Room)
	// The token goes in the Authorization header, never the URL (which
	// proxies and servers log).
	q := url.Values{}
	if o.Lang != "" {
		q.Set("lang", o.Lang)
	}
	if o.Talk != "" {
		q.Set("talk", o.Talk)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Stream sends src (raw PCM16 mono 16 kHz) to the room until src ends or
// ctx is done. Lost connections, a busy room and failed transcription
// sessions are retried with backoff; only a rejected token, an unknown
// room or a read error on src end it early.
func Stream(ctx context.Context, opt Options, src io.Reader) error {
	endpoint, err := opt.Endpoint()
	if err != nil {
		return err
	}
	log := opt.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	minBackoff, maxBackoff := opt.MinBackoff, opt.MaxBackoff
	if minBackoff <= 0 {
		minBackoff = time.Second
	}
	if maxBackoff < minBackoff {
		maxBackoff = max(30*time.Second, minBackoff)
	}

	chunks := make(chan []byte, backlog)
	srcErr := make(chan error, 1)
	go func() { srcErr <- read(ctx, src, chunks, opt.Realtime) }()

	backoff := minBackoff
	for {
		sent, err := session(ctx, endpoint, opt.Token, chunks, log)
		switch {
		case err == nil: // src ended and the room closed cleanly
			return <-srcErr
		case errors.Is(err, ErrUnauthorized), errors.Is(err, ErrUnknownRoom), errors.Is(err, ErrLanguage), errors.Is(err, ErrEnded):
			return err
		case ctx.Err() != nil:
			return nil
		}
		if sent > 0 {
			backoff = minBackoff // it worked for a while: retry soon
		}
		log.Warn("ingest interrupted; reconnecting", "room", opt.Room, "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, maxBackoff)
	}
}

// session streams chunks over one connection and returns how many it sent.
// A nil error means chunks was closed and the socket closed normally.
func session(ctx context.Context, endpoint, token string, chunks <-chan []byte, log *slog.Logger) (int, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	var opts *websocket.DialOptions
	if token != "" {
		opts = &websocket.DialOptions{HTTPHeader: http.Header{"X-Dubash-Token": {token}}}
	}
	c, resp, err := websocket.Dial(dialCtx, endpoint, opts)
	cancel()
	if err != nil {
		if resp != nil {
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				return 0, ErrUnauthorized
			case http.StatusNotFound:
				return 0, ErrUnknownRoom
			case http.StatusBadRequest:
				return 0, ErrLanguage
			case http.StatusConflict:
				return 0, errors.New("room busy: another operator is streaming")
			}
		}
		if errors.Is(err, syscall.ECONNREFUSED) {
			return 0, fmt.Errorf("nothing is listening at %s: is the Dubash server running? (%w)", redact(endpoint), err)
		}
		return 0, err
	}
	defer c.CloseNow()
	log.Info("ingest connected", "endpoint", redact(endpoint))

	// The server sends no data messages; reading surfaces its close frame
	// (room busy, transcription failed) while the loop below is writing.
	closed := make(chan error, 1)
	go func() {
		_, _, err := c.Read(ctx)
		closed <- err
	}()

	sent := 0
	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return sent, ctx.Err()
		case err := <-closed:
			return sent, closeError(err)
		case chunk, ok := <-chunks:
			if !ok {
				// The audio went out; the server drains the rest on its own.
				c.Close(websocket.StatusNormalClosure, "")
				return sent, nil
			}
			if err := c.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				return sent, err
			}
			sent++
		}
	}
}

func closeError(err error) error {
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure:
		return errors.New("server closed the session")
	case websocket.StatusPolicyViolation:
		return errors.New("room busy: another operator is streaming")
	case statusEventEnded:
		return ErrEnded
	}
	var ce websocket.CloseError
	if errors.As(err, &ce) && ce.Reason != "" {
		return errors.New(ce.Reason)
	}
	return err
}

// read cuts src into ChunkBytes chunks. When the backlog is full (the
// server is unreachable) it drops the oldest chunk, so audio stays live.
func read(ctx context.Context, src io.Reader, out chan []byte, realtime bool) error {
	defer close(out)
	br := bufio.NewReaderSize(src, 4*ChunkBytes)
	start := time.Now()
	var total time.Duration
	for {
		buf := make([]byte, ChunkBytes)
		n, err := io.ReadFull(br, buf)
		n -= n % 2 // whole samples only
		if n > 0 {
			if realtime {
				total += time.Duration(n) * time.Second / asr.BytesPerSecond
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(time.Until(start.Add(total))):
				}
			}
			push(out, buf[:n])
		}
		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return nil
		case err != nil:
			return fmt.Errorf("ingest: read audio: %w", err)
		case ctx.Err() != nil:
			return nil
		}
	}
}

func push(out chan []byte, chunk []byte) {
	for {
		select {
		case out <- chunk:
			return
		default:
		}
		select {
		case <-out: // drop the oldest
		default:
		}
	}
}

// redact hides the token in logs.
func redact(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "?"
	}
	u.RawQuery = ""
	return u.String()
}
