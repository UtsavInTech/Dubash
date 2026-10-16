// Copyright 2026 the Dubash contributors. Apache-2.0.

package asr

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestReplaySarvamCapture feeds a capture made with cmd/sarvam-smoke
// through the engine's message handling, offline, and prints the finals:
//
//	SARVAM_REPLAY=capture.txt go test -run ReplaySarvam -v ./internal/asr
//
// Captures hold real transcripts, so they stay out of the repository.
func TestReplaySarvamCapture(t *testing.T) {
	path := os.Getenv("SARVAM_REPLAY")
	if path == "" {
		t.Skip("set SARVAM_REPLAY to a cmd/sarvam-smoke capture")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	events := make(chan Event, 100000)
	s := &Sarvam{Early: os.Getenv("SARVAM_EARLY") == "on"}
	r := &sarvamRun{s: s, ctx: context.Background(), events: events}
	r.asm.finalsOnly = !s.Early
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		i := strings.Index(line, "{")
		if i < 0 {
			continue
		}
		var m sarvamMsg
		if json.Unmarshal([]byte(line[i:]), &m) != nil {
			continue
		}
		r.handle(m)
	}
	r.emitAll(r.asm.flush())
	close(events)
	words := 0
	for e := range events {
		if e.Kind == Final {
			words += len(strings.Fields(e.Text))
			t.Logf("FINAL %q", e.Text)
		}
	}
	t.Logf("%d words in finals", words)
}
