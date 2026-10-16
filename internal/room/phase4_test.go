// Copyright 2026 the Dubash contributors. Apache-2.0.

package room

import (
	"fmt"
	"testing"
)

// Slow readers must be disconnected without delaying the other 500 readers.
// Their last delivered cursor must still recover all missed committed lines.
func TestHubLoadIsolatesSlowReadersAndReplays(t *testing.T) {
	h := NewHub(50, nil)
	fast := make([]<-chan Msg, 500)
	var cancel []func()
	for i := range fast {
		ch, _, stop := h.Subscribe(8, "en", "")
		fast[i] = ch
		cancel = append(cancel, stop)
	}
	defer func() {
		for _, stop := range cancel {
			stop()
		}
	}()
	first := h.Publish(final(0))
	for _, ch := range fast {
		<-ch
	}
	slow := make([]<-chan Msg, 500)
	for i := range slow {
		ch, _, stop := h.Subscribe(8, "en", first.ID(h.Instance()))
		slow[i] = ch
		cancel = append(cancel, stop)
	}
	for i := 1; i <= 25; i++ {
		h.Publish(final(i))
		h.Publish(tr(i, "en"))
		for _, ch := range fast {
			a, b := <-ch, <-ch
			if a.Segment != i || b.Segment != i {
				t.Fatalf("fast reader lost line %d", i)
			}
		}
	}
	if h.Viewers() != 500 {
		t.Fatalf("slow readers retained: %d", h.Viewers())
	}
	for _, ch := range slow {
		for range ch {
		}
	}
	_, replay, stop := h.Subscribe(8, "en", first.ID(h.Instance()))
	defer stop()
	if len(replay) != 50 {
		t.Fatalf("resume lost committed messages: %d", len(replay))
	}
	for i, m := range replay {
		if m.Segment != i/2+1 {
			t.Fatalf("replay order %d: %+v", i, m)
		}
	}
}

func BenchmarkHubFanout(b *testing.B) {
	for _, readers := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprint(readers), func(b *testing.B) {
			h := NewHub(50, nil)
			channels := make([]<-chan Msg, readers)
			for i := range channels {
				ch, _, stop := h.Subscribe(1, "en", "")
				channels[i] = ch
				b.Cleanup(stop)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				h.Publish(final(i))
				for _, ch := range channels {
					<-ch
				}
			}
		})
	}
}
