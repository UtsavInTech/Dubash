// Modified by the Dubash contributors: per-language subscriptions,
// sequence numbers and resume.

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

func final(id int) Msg { return Msg{Event: "final", Data: seg(id), Segment: id} }

func tr(id int, lang string) Msg {
	return Msg{Event: "translation", Lang: lang, Segment: id,
		Data: caption.Translation{Segment: id, Lang: lang, Text: fmt.Sprintf("[%s] line %d", lang, id)}}
}

// ids lists the segment of each message, prefixed by its language.
func ids(ms []Msg) []string {
	var out []string
	for _, m := range ms {
		out = append(out, fmt.Sprintf("%s%d", m.Lang, m.Segment))
	}
	return out
}

func TestHubDeliversToAllSubscribers(t *testing.T) {
	h := NewHub(10, nil)
	a, _, cancelA := h.Subscribe(4, "", "")
	defer cancelA()
	b, _, cancelB := h.Subscribe(4, "hi", "")
	defer cancelB()
	h.Publish(final(1))
	for _, ch := range []<-chan Msg{a, b} {
		m := <-ch
		if m.Event != "final" || m.Data.(caption.Segment).ID != 1 || m.Seq != 1 {
			t.Fatalf("got %+v", m)
		}
	}
	if h.Viewers() != 2 {
		t.Fatalf("viewers = %d", h.Viewers())
	}
}

func TestHubSendsTranslationsOnlyToTheirReaders(t *testing.T) {
	h := NewHub(10, nil)
	hi, _, c1 := h.Subscribe(4, "hi", "")
	defer c1()
	te, _, c2 := h.Subscribe(4, "te", "")
	defer c2()
	h.Publish(tr(1, "hi"))
	if m := <-hi; m.Event != "translation" || m.Lang != "hi" {
		t.Fatalf("hi reader got %+v", m)
	}
	if len(te) != 0 {
		t.Fatal("te reader got a Hindi translation")
	}
	if !h.Reading("hi") || h.Reading("kn") {
		t.Fatal("Reading")
	}
	if l := h.Listeners(); l["hi"] != 1 || l["te"] != 1 {
		t.Fatalf("listeners %v", l)
	}
}

func TestHubReplaysLastSegmentsInTheReadersLanguage(t *testing.T) {
	seed := []caption.Segment{{ID: 0, Text: "zero", Translations: map[string]string{"hi": "शून्य", "te": "సున్నా"}}}
	h := NewHub(3, seed)
	for i := 1; i <= 4; i++ {
		h.Publish(final(i))
		h.Publish(tr(i, "hi"))
		h.Publish(tr(i, "te"))
	}
	h.Publish(Msg{Event: "interim", Data: caption.Interim{Text: "x"}})
	_, replay, cancel := h.Subscribe(1, "hi", "")
	defer cancel()
	if got := ids(replay); !slices.Equal(got, []string{"2", "hi2", "3", "hi3", "4", "hi4"}) {
		t.Fatalf("replay %v", got)
	}
	// The store's translations come back with the segments.
	recent := NewHub(3, seed).Recent()
	if len(recent) != 1 || recent[0].Translations["te"] != "సున్నా" {
		t.Fatalf("recent %+v", recent)
	}
}

func TestHubResumesAfterTheLastIDSeen(t *testing.T) {
	h := NewHub(50, nil)
	sub, _, cancel := h.Subscribe(16, "hi", "")
	h.Publish(final(1))
	h.Publish(tr(1, "hi"))
	m1 := <-sub
	last := <-sub
	cancel() // the phone loses its connection here
	if m1.Seq != 1 || last.ID(h.Instance()) == "" {
		t.Fatalf("ids: %+v %+v", m1, last)
	}
	h.Publish(final(2))
	h.Publish(tr(2, "te"))
	h.Publish(tr(2, "hi"))
	_, replay, cancel := h.Subscribe(16, "hi", last.ID(h.Instance()))
	defer cancel()
	if got := ids(replay); !slices.Equal(got, []string{"2", "hi2"}) {
		t.Fatalf("resume replay %v", got)
	}
	// An id from another server instance means start over.
	_, replay, cancel2 := h.Subscribe(16, "hi", "old-2")
	defer cancel2()
	if got := ids(replay); !slices.Equal(got, []string{"1", "hi1", "2", "hi2"}) {
		t.Fatalf("stale id replay %v", got)
	}
}

func TestHubDropsSlowSubscriber(t *testing.T) {
	h := NewHub(10, nil)
	slow, _, cancel := h.Subscribe(1, "", "")
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
	_, _, cancel := h.Subscribe(1, "", "")
	cancel()
	cancel()
	if h.Viewers() != 0 {
		t.Fatalf("viewers = %d", h.Viewers())
	}
}
