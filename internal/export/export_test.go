package export

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/caption"
)

var segs = []caption.Segment{
	{ID: 0, T0: 0, T1: 2.5, Lang: "en", Text: "Hello.", Translations: map[string]string{"es": "Hola."}},
	{ID: 1, T0: 2.5, T1: 3723.456, Lang: "en", Text: "Bye."},
}

func TestVTT(t *testing.T) {
	want := "WEBVTT\n\n" +
		"1\n00:00:00.000 --> 00:00:02.500\nHola.\n\n" +
		"2\n00:00:02.500 --> 01:02:03.456\nBye.\n\n"
	if got := VTT(segs, "es"); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSRT(t *testing.T) {
	want := "1\n00:00:00,000 --> 00:00:02,500\nHello.\n\n" +
		"2\n00:00:02,500 --> 01:02:03,456\nBye.\n\n"
	if got := SRT(segs, "en"); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestTXT(t *testing.T) {
	if got := TXT(segs, "es"); got != "Hola.\nBye.\n" {
		t.Fatalf("got %q", got)
	}
}

func TestTalksAndSelect(t *testing.T) {
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	segs := []caption.Segment{
		{ID: 0, Talk: 1, T0: 0, T1: 4, Text: "a"},
		{ID: 1, Talk: 1, T0: 4, T1: 9, Text: "b"},
		{ID: 2, Talk: 2, TalkTitle: "eBPF", At: at, T0: 9, T1: 12, Text: "c"},
		{ID: 3, Talk: 2, TalkTitle: "eBPF", T0: 12, T1: 20.5, Text: "d"},
	}
	want := []Talk{
		{N: 1, Segments: 2, Seconds: 9},
		{N: 2, Title: "eBPF", Start: at, Segments: 2, Seconds: 11.5},
	}
	if got := Talks(segs); !slices.Equal(got, want) {
		t.Fatalf("Talks = %+v, want %+v", got, want)
	}
	got := Select(segs, 2)
	if len(got) != 2 || got[0].T0 != 0 || got[0].T1 != 3 || got[1].T1 != 11.5 {
		t.Fatalf("Select = %+v", got)
	}
	if segs[2].T0 != 9 {
		t.Fatal("Select modified its input")
	}
	if Select(segs, 3) != nil {
		t.Fatal("Select of a missing talk should be nil")
	}
}

func ExampleSRT() {
	segs := []caption.Segment{
		{T0: 0, T1: 2.5, Lang: "en", Text: "Welcome to Nerdearla.", Translations: map[string]string{"es": "Bienvenidos a Nerdearla."}},
		{T0: 2.5, T1: 5, Lang: "en", Text: "Let's talk about eBPF."},
	}
	fmt.Print(SRT(segs, "es"))
	// Output:
	// 1
	// 00:00:00,000 --> 00:00:02,500
	// Bienvenidos a Nerdearla.
	//
	// 2
	// 00:00:02,500 --> 00:00:05,000
	// Let's talk about eBPF.
}
