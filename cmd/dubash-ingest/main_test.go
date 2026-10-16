package main

import (
	"slices"
	"testing"
)

// ffmpeg analyzes 5 s of a live stream before its first output by default,
// which held back the first caption each time OBS started recording.
func TestFFmpegStartsDecodingQuickly(t *testing.T) {
	args := ffmpegArgs("udp://127.0.0.1:5000")
	in := slices.Index(args, "-i")
	if in < 0 || args[in+1] != "udp://127.0.0.1:5000" {
		t.Fatalf("input missing: %q", args)
	}
	for _, opt := range []string{"-probesize", "-analyzeduration"} {
		i := slices.Index(args, opt)
		if i < 0 || i > in {
			t.Errorf("%s must be set before -i: %q", opt, args)
		}
	}
}
