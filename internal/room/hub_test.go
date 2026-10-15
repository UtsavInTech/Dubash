package room

import (
	"fmt"
	"slices"
	"testing"

	"github.com/UtsavInTech/dubash/internal/caption"
)

func seg(id int) caption.Segment {
	return caption.Segment{ID: id, Text: fmt.Sprint("line ", id)}
}

func TestHubDeliversToAllSubscribers(t *testing.T) {
	h := NewHub(10, nil)
	a, _, cancelA := h.Subscribe(4)
	defer cancelA()
	b, _, cancelB := h.Subscribe(4)
	defer cancelB()
	h.Publish(Msg{Event: "final", Data: seg(1)})
	for _, ch := range []<-chan Msg{a, b} {
		m := <-ch
		if m.Event != "final" || m.Data.(caption.Segment).ID != 1 {
			t.Fatalf("got %+v", m)
		}
	}
	if h.Viewers() != 2 {
		t.Fatalf("viewers = %d", h.Viewers())
	}
}

func TestHubRingKeepsLastFinals(t *testing.T) {
	h := NewHub(3, []caption.Segment{seg(0)})
	for i := 1; i <= 4; i++ {
		h.Publish(Msg{Event: "final", Data: seg(i)})
	}
	h.Publish(Msg{Event: "interim", Data: map[string]string{"text": "x"}})
	_, snap, cancel := h.Subscribe(1)
	defer cancel()
	var ids []int
	for _, s := range snap {
		ids = append(ids, s.ID)
	}
	if !slices.Equal(ids, []int{2, 3, 4}) {
		t.Fatalf("snapshot ids = %v", ids)
	}
}

func TestHubDropsSlowSubscriber(t *testing.T) {
	h := NewHub(10, nil)
	slow, _, cancel := h.Subscribe(1)
	defer cancel()
	h.Publish(Msg{Event: "interim"})
	h.Publish(Msg{Event: "interim"}) // overflows the buffer of 1
	<-slow                           // the buffered message is still readable
	if _, ok := <-slow; ok {
		t.Fatal("expected the slow subscriber's channel to be closed")
	}
	if h.Viewers() != 0 {
		t.Fatalf("viewers = %d, want 0", h.Viewers())
	}
}

func TestHubCancelIsIdempotent(t *testing.T) {
	h := NewHub(1, nil)
	_, _, cancel := h.Subscribe(1)
	cancel()
	cancel()
	if h.Viewers() != 0 {
		t.Fatalf("viewers = %d", h.Viewers())
	}
}
