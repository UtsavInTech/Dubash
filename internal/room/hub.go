// Modified by the Dubash contributors: subscriptions per language,
// sequence numbers, resume from the last message seen, and listener
// counts per language.

package room

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/UtsavInTech/dubash/internal/caption"
)

// Msg is one event sent to viewers: "status", "interim", "final" (a
// committed source line), "translation" or "correction".
type Msg struct {
	Event string
	Data  any
	// Lang limits the message to subscribers of that language; empty
	// means everyone.
	Lang string
	// Seq numbers committed messages (final, translation, correction) in
	// the order published; 0 for status and interim, which are never
	// replayed.
	Seq int64
	// Segment is the segment a committed message is about.
	Segment int
}

// ID is the message's Server-Sent Events id: the hub's instance and the
// sequence number. Empty for messages that are not replayed.
func (m Msg) ID(instance string) string {
	if m.Seq == 0 {
		return ""
	}
	return instance + "-" + strconv.FormatInt(m.Seq, 10)
}

// committed are the events a hub numbers and replays.
func committed(event string) bool {
	return event == "final" || event == "translation" || event == "correction"
}

// Hub fans room events out to viewers and remembers the recent committed
// messages, so a late joiner catches up on the last segments and a
// reconnecting viewer gets exactly what it missed. Publish never blocks:
// a subscriber whose buffer is full is dropped (its channel is closed)
// and its browser reconnects on its own.
type Hub struct {
	mu       sync.Mutex
	subs     map[*sub]struct{}
	ring     []Msg // committed messages, oldest first
	seq      int64
	size     int    // segments a new viewer receives
	maxRing  int    // messages kept for resuming
	instance string // changes on restart, so stale ids are recognized
}

type sub struct {
	ch   chan Msg
	lang string
}

// ringPerSegment bounds the resume buffer: a segment's source line plus
// a translation per language and a correction or two.
const ringPerSegment = 24

// NewHub replays the last size segments to new viewers, starting from
// seed (segments loaded from the store, with their translations).
func NewHub(size int, seed []caption.Segment) *Hub {
	h := &Hub{
		subs: map[*sub]struct{}{}, size: size, maxRing: size * ringPerSegment,
		instance: strconv.FormatInt(time.Now().UnixMilli(), 36),
	}
	if len(seed) > size {
		seed = seed[len(seed)-size:]
	}
	for _, s := range seed {
		h.remember(Msg{Event: "final", Data: s, Segment: s.ID})
		for _, l := range slices.Sorted(maps.Keys(s.Translations)) {
			h.remember(Msg{Event: "translation", Lang: l, Segment: s.ID, Data: caption.Translation{
				Room: s.Room, Segment: s.ID, Epoch: s.Epoch, Rev: s.Rev, Lang: l, Text: s.Translations[l],
			}})
		}
	}
	return h
}

// Instance identifies this hub's sequence numbers.
func (h *Hub) Instance() string { return h.instance }

// remember numbers m and keeps it for replay. The caller holds mu (or
// owns h exclusively).
func (h *Hub) remember(m Msg) Msg {
	h.seq++
	m.Seq = h.seq
	h.ring = append(h.ring, m)
	if len(h.ring) > h.maxRing {
		h.ring = slices.Clone(h.ring[len(h.ring)-h.maxRing:])
	}
	return m
}

func (s *sub) wants(m Msg) bool { return m.Lang == "" || m.Lang == s.lang }

// Subscribe returns live messages for a viewer reading lang (empty for
// the source language only), the committed messages to replay first, and
// a cancel func (safe to call more than once).
//
// lastID is the id of the last message the viewer received (the SSE
// Last-Event-ID). When it belongs to this hub and is still in the resume
// buffer, the replay is exactly what came after it; otherwise it is the
// last segments, which the viewer deduplicates by segment id.
func (h *Hub) Subscribe(buf int, lang, lastID string) (<-chan Msg, []Msg, func()) {
	s := &sub{ch: make(chan Msg, buf), lang: lang}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	replay := h.replay(s, lastID)
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[s]; ok {
			delete(h.subs, s)
			close(s.ch)
		}
	}
	return s.ch, replay, cancel
}

// replay picks the messages s needs first. The caller holds mu.
func (h *Hub) replay(s *sub, lastID string) []Msg {
	if after, ok := h.parseID(lastID); ok && len(h.ring) > 0 && after >= h.ring[0].Seq-1 {
		var out []Msg
		for _, m := range h.ring {
			if m.Seq > after && s.wants(m) {
				out = append(out, m)
			}
		}
		return out
	}
	// The last size segments.
	var ids []int
	for _, m := range h.ring {
		if m.Event == "final" && !slices.Contains(ids, m.Segment) {
			ids = append(ids, m.Segment)
		}
	}
	if len(ids) > h.size {
		ids = ids[len(ids)-h.size:]
	}
	var out []Msg
	for _, m := range h.ring {
		if s.wants(m) && slices.Contains(ids, m.Segment) {
			out = append(out, m)
		}
	}
	return out
}

func (h *Hub) parseID(id string) (int64, bool) {
	inst, seq, ok := strings.Cut(id, "-")
	if !ok || inst != h.instance {
		return 0, false
	}
	n, err := strconv.ParseInt(seq, 10, 64)
	return n, err == nil && n <= h.seq
}

// Publish delivers m to every subscriber that wants it, numbering and
// remembering committed messages. It returns m as delivered.
func (h *Hub) Publish(m Msg) Msg {
	h.mu.Lock()
	defer h.mu.Unlock()
	if committed(m.Event) {
		m = h.remember(m)
	}
	for s := range h.subs {
		if !s.wants(m) {
			continue
		}
		select {
		case s.ch <- m:
		default:
			delete(h.subs, s)
			close(s.ch)
		}
	}
	return m
}

// Viewers is the number of connected subscribers.
func (h *Hub) Viewers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Listeners counts the connected subscribers per language they read
// ("" for viewers of the original only).
func (h *Hub) Listeners() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]int{}
	for s := range h.subs {
		out[s.lang]++
	}
	return out
}

// Reading reports whether anyone reads lang now.
func (h *Hub) Reading(lang string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if s.lang == lang {
			return true
		}
	}
	return false
}

// Recent returns the remembered segments, oldest first, with the
// translations remembered for them.
func (h *Hub) Recent() []caption.Segment {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []caption.Segment
	index := map[int]int{}
	for _, m := range h.ring {
		switch d := m.Data.(type) {
		case caption.Segment:
			if i, ok := index[d.ID]; ok { // a correction replaces the line
				tr := out[i].Translations
				out[i] = d
				out[i].Translations = tr
				continue
			}
			index[d.ID] = len(out)
			d.Translations = maps.Clone(d.Translations)
			if d.Translations == nil {
				d.Translations = map[string]string{}
			}
			out = append(out, d)
		case caption.Translation:
			if i, ok := index[d.Segment]; ok && d.Text != "" {
				out[i].Translations[d.Lang] = d.Text
			}
		}
	}
	return out
}

// String describes the hub for logs.
func (h *Hub) String() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return fmt.Sprintf("hub %s: %d subscribers, %d messages, seq %d", h.instance, len(h.subs), len(h.ring), h.seq)
}
