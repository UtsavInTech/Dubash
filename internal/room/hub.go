package room

import (
	"slices"
	"sync"

	"github.com/UtsavInTech/dubash/internal/caption"
)

// Msg is one event sent to viewers: "interim", "final" or "status".
type Msg struct {
	Event string
	Data  any
}

// Hub fans room events out to viewers and remembers the last finals so
// late joiners can catch up. Publish never blocks: a subscriber whose
// buffer is full is dropped (its channel is closed) and its browser
// reconnects on its own.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Msg]struct{}
	ring []caption.Segment
	size int
}

// NewHub keeps the last size finals, starting from seed.
func NewHub(size int, seed []caption.Segment) *Hub {
	h := &Hub{subs: map[chan Msg]struct{}{}, size: size}
	for _, s := range seed {
		h.remember(s)
	}
	return h
}

func (h *Hub) remember(s caption.Segment) {
	h.ring = append(h.ring, s)
	if len(h.ring) > h.size {
		h.ring = slices.Clone(h.ring[len(h.ring)-h.size:])
	}
}

// Subscribe returns live messages, a snapshot of recent finals to replay
// first, and a cancel func (safe to call more than once).
func (h *Hub) Subscribe(buf int) (<-chan Msg, []caption.Segment, func()) {
	ch := make(chan Msg, buf)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	snap := slices.Clone(h.ring)
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
	return ch, snap, cancel
}

// Publish delivers m to every subscriber.
func (h *Hub) Publish(m Msg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := m.Data.(caption.Segment); ok && m.Event == "final" {
		h.remember(s)
	}
	for ch := range h.subs {
		select {
		case ch <- m:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Viewers is the number of connected subscribers.
func (h *Hub) Viewers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Recent returns the remembered finals, oldest first.
func (h *Hub) Recent() []caption.Segment {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.ring)
}
