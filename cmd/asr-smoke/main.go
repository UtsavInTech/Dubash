// Command asr-smoke streams raw PCM16 16 kHz mono from stdin to the Gemini
// Live transcription engine in real time and prints the events. Use it to
// check the protocol and tune the assembler:
//
//	ffmpeg -loglevel error -i samples/en.wav -f s16le -ac 1 -ar 16000 - |
//	  go run ./cmd/asr-smoke -lang en -debug
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"time"

	"google.golang.org/genai"

	"github.com/UtsavInTech/dubash/internal/asr"
)

func main() {
	lang := flag.String("lang", "en", "spoken language: en, es, pt, fr, de or it")
	model := flag.String("model", "", "Live model (default "+asr.DefaultModel+")")
	debug := flag.Bool("debug", false, "log raw server messages")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: os.Getenv("GEMINI_API_KEY"), Backend: genai.BackendGeminiAPI})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	audio := make(chan []byte, 50)
	go func() {
		defer close(audio)
		in := bufio.NewReader(os.Stdin)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for range tick.C {
			buf := make([]byte, asr.BytesPerSecond/10)
			n, err := io.ReadFull(in, buf)
			if n > 0 {
				audio <- buf[:n]
			}
			if err != nil {
				return
			}
		}
	}()

	events := make(chan asr.Event, 64)
	var wg sync.WaitGroup
	wg.Go(func() {
		names := map[asr.Kind]string{asr.Interim: "interim", asr.Final: "FINAL", asr.Error: "error"}
		for ev := range events {
			fmt.Printf("%7.1fs %-7s %s\n", ev.At, names[ev.Kind], ev.Text)
		}
	})
	eng := &asr.Gemini{Client: client, Model: *model, Debug: *debug, Log: slog.Default()}
	err = eng.Run(ctx, asr.Config{Lang: *lang, Vocabulary: []string{"Nerdearla"}}, audio, events)
	close(events)
	wg.Wait()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
