// Package export renders stored segments as subtitle files.
package export

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/UtsavInTech/dubash/internal/caption"
)

// VTT renders WebVTT in the given language.
func VTT(segs []caption.Segment, lang string) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	cues(&b, segs, lang, ".")
	return b.String()
}

// SRT renders SubRip in the given language.
func SRT(segs []caption.Segment, lang string) string {
	var b strings.Builder
	cues(&b, segs, lang, ",")
	return b.String()
}

// TXT renders one line per segment in the given language.
func TXT(segs []caption.Segment, lang string) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.In(lang))
		b.WriteByte('\n')
	}
	return b.String()
}

func cues(b *strings.Builder, segs []caption.Segment, lang, sep string) {
	for i, s := range segs {
		fmt.Fprintf(b, "%d\n%s --> %s\n%s\n\n", i+1, stamp(s.T0, sep), stamp(s.T1, sep), s.In(lang))
	}
}

// stamp formats seconds as HH:MM:SS<sep>mmm.
func stamp(sec float64, sep string) string {
	ms := int64(math.Round(sec * 1000))
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, sep, ms%1000)
}

// Talk summarizes one talk of a room's transcript.
type Talk struct {
	N        int       `json:"talk"`
	Title    string    `json:"title,omitempty"`
	Start    time.Time `json:"start,omitzero"` // when its first line was transcribed
	Segments int       `json:"segments"`
	Seconds  float64   `json:"seconds"` // audio covered, first line to last
}

// Talks lists the talks in segs, in order of appearance.
func Talks(segs []caption.Segment) []Talk {
	var out []Talk
	var start float64 // T0 of the current talk's first line
	for _, s := range segs {
		if len(out) == 0 || out[len(out)-1].N != s.Talk {
			out = append(out, Talk{N: s.Talk, Title: s.TalkTitle, Start: s.At})
			start = s.T0
		}
		t := &out[len(out)-1]
		t.Segments++
		t.Seconds = s.T1 - start
	}
	return out
}

// Select returns the segments of talk n with their times shifted so the
// talk starts at zero, ready to be played over a recording of that talk.
func Select(segs []caption.Segment, n int) []caption.Segment {
	var out []caption.Segment
	for _, s := range segs {
		if s.Talk == n {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	start := out[0].T0
	for i := range out {
		out[i].T0 -= start
		out[i].T1 -= start
	}
	return out
}
