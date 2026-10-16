package asr

import (
	"context"
	"testing"
)

func TestFakeEmitsInterimThenFinal(t *testing.T) {
	audio := make(chan []byte)
	events := make(chan Event, 16)
	go func() {
		for range 10 { // 1 s in 100 ms chunks
			audio <- make([]byte, BytesPerSecond/10)
		}
		close(audio)
	}()
	if err := (Fake{Every: 1}).Run(context.Background(), Config{Lang: "en"}, audio, events); err != nil {
		t.Fatal(err)
	}
	close(events)
	var got []Event
	for e := range events {
		got = append(got, e)
	}
	want := []Event{
		{Kind: Interim, Text: "Welcome to", At: 0.5},
		{Kind: Final, Text: "Welcome to Nerdearla.", At: 1},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFakeUsesRoomLanguage(t *testing.T) {
	audio := make(chan []byte, 1)
	events := make(chan Event, 4)
	audio <- make([]byte, BytesPerSecond)
	close(audio)
	_ = Fake{Every: 1}.Run(context.Background(), Config{Lang: "es"}, audio, events)
	close(events)
	var last Event
	for e := range events {
		last = e
	}
	if last.Text != DefaultLines["es"][0] {
		t.Fatalf("got %q", last.Text)
	}
}
