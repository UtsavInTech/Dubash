// Command dubash-ingest feeds a stage's audio into a Dubash room
// without a browser. Give it anything ffmpeg can read, or pipe raw PCM16
// mono 16 kHz on stdin:
//
//	dubash-ingest -server https://subs.example.org -room sala-a -i srt://mixer:9000
//	dubash-ingest -room sala-a -lang es -talk "eBPF en producción" -i talk.mp4 -realtime
//	ffmpeg -i rtmp://… -f s16le -ac 1 -ar 16000 - | dubash-ingest -room sala-a
//
// It reconnects on its own when the network, the server or the source
// drops, so one process per stage can run unattended for a whole event.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/UtsavInTech/dubash/internal/ingest"
)

func main() {
	server := flag.String("server", envOr("DUBASH_URL", "http://localhost:8080"), "Dubash base URL (env DUBASH_URL)")
	roomID := flag.String("room", "", "room id from rooms.yaml (required)")
	token := flag.String("token", os.Getenv("ADMIN_TOKEN"), "operator token (env ADMIN_TOKEN)")
	lang := flag.String("lang", "", "language spoken on stage: en, es, pt, fr, de or it (default: the room's current one)")
	talk := flag.String("talk", "", "title of the talk; a new title starts a new talk in the transcript")
	input := flag.String("i", "", "input for ffmpeg: a URL (srt://, rtmp://, https://…m3u8), a device or a file; empty reads PCM from stdin")
	realtime := flag.Bool("realtime", false, "pace the input at playback speed (for files)")
	ffmpeg := flag.String("ffmpeg", "ffmpeg", "ffmpeg binary")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *roomID == "" {
		fmt.Fprintln(os.Stderr, "dubash-ingest: -room is required")
		flag.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opt := ingest.Options{Server: *server, Room: *roomID, Token: *token, Lang: *lang, Talk: *talk, Realtime: *realtime, Log: log}

	var err error
	if *input == "" {
		err = ingest.Stream(ctx, opt, os.Stdin)
	} else {
		err = fromFFmpeg(ctx, opt, *ffmpeg, *input, log)
	}
	if err != nil {
		log.Error("ingest stopped", "err", err)
		os.Exit(1)
	}
}

// fromFFmpeg decodes input with ffmpeg and streams it, restarting ffmpeg
// when a live source drops. A clean ffmpeg exit (the file ended) stops.
func fromFFmpeg(ctx context.Context, opt ingest.Options, bin, input string, log *slog.Logger) error {
	backoff := time.Second
	for {
		runCtx, cancel := context.WithCancel(ctx)
		cmd := exec.CommandContext(runCtx, bin, ffmpegArgs(input)...)
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			cancel()
			return err
		}
		if err := cmd.Start(); err != nil {
			cancel()
			return fmt.Errorf("start ffmpeg: %w", err)
		}
		started := time.Now()
		streamErr := ingest.Stream(ctx, opt, out)
		if streamErr != nil {
			cancel() // ffmpeg would block on a pipe nobody reads
		}
		waitErr := cmd.Wait()
		cancel()
		switch {
		case ctx.Err() != nil:
			return nil
		case errors.Is(streamErr, ingest.ErrUnauthorized), errors.Is(streamErr, ingest.ErrUnknownRoom), errors.Is(streamErr, ingest.ErrLanguage):
			return streamErr
		case waitErr == nil && streamErr == nil:
			return nil
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		log.Warn("audio source ended; restarting ffmpeg", "err", errors.Join(waitErr, streamErr), "in", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 30*time.Second)
	}
}

// ffmpegArgs decodes input to 16 kHz mono PCM on stdout. By default ffmpeg
// analyzes 5 s of a live stream before its first output; half a second
// (or 500 KB) is enough to find the audio and starts captions ~4.5 s sooner.
func ffmpegArgs(input string) []string {
	return []string{"-hide_banner", "-loglevel", "error", "-nostdin",
		"-probesize", "500000", "-analyzeduration", "500000",
		"-i", input, "-vn", "-f", "s16le", "-ac", "1", "-ar", "16000", "-"}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
