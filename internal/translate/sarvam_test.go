// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMaskUnmask(t *testing.T) {
	masked, slots := Mask("Welcome to Dubash, by Sarvam AI and sarvam. Dubashi is another word. बागेश्वर धाम में", Keep("Sarvam", "Sarvam AI", "Dubash", "बागेश्वर धाम", " "))
	// Longest terms are masked first and numbered first; "sarvam" is put
	// back as the glossary writes it.
	want := "Welcome to [4], by [2] and [3]. Dubashi is another word. [1] में"
	if masked != want {
		t.Errorf("masked %q\nwant   %q", masked, want)
	}
	if strings.Join(slots, "|") != "बागेश्वर धाम|Sarvam AI|Sarvam|Dubash" {
		t.Errorf("slots %q", slots)
	}
	back, ok := Unmask("[4] में [2] और [3] [1]", slots)
	if !ok || back != "Dubash में Sarvam AI और Sarvam बागेश्वर धाम" {
		t.Errorf("unmask %q %v", back, ok)
	}
	if _, ok := Unmask("[1] [2] [3]", slots); ok {
		t.Error("a missing placeholder went unnoticed")
	}
	// Ten or more slots: [1] must not eat the start of [10].
	terms := []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10"}
	m, s := Mask(strings.Join(terms, " "), Keep(terms...))
	if back, ok := Unmask(m, s); !ok || back != strings.Join(terms, " ") {
		t.Errorf("ten slots: %q → %q", m, back)
	}
}

func TestSarvamTranslator(t *testing.T) {
	var calls atomic.Int32
	var inputs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		inputs = append(inputs, body["input"])
		if r.Header.Get("api-subscription-key") != "k" || body["target_language_code"] != "te-IN" || body["source_language_code"] != "hi-IN" {
			t.Errorf("request %v", body)
		}
		out := strings.ReplaceAll(body["input"], "स्वागत", "స్వాగతం")
		if strings.Contains(body["input"], "drop") {
			out = "స్వాగతం" // loses the placeholder
		}
		json.NewEncoder(w).Encode(map[string]string{"translated_text": out})
	}))
	defer srv.Close()
	s := Sarvam{Key: "k", URL: srv.URL}

	got, err := s.Translate(context.Background(), "Dubash में स्वागत", "hi", "te", Keep("Dubash"))
	if err != nil || got != "Dubash में స్వాగతం" || inputs[0] != "[1] में स्वागत" {
		t.Errorf("got %q err %v sent %q", got, err, inputs)
	}
	// A lost placeholder: the line is translated again, unmasked.
	inputs = nil
	got, err = s.Translate(context.Background(), "drop Dubash", "hi", "te", Keep("Dubash"))
	if err != nil || len(inputs) != 2 || inputs[1] != "drop Dubash" {
		t.Errorf("fallback: %q %v %q", got, err, inputs)
	}
	if _, err := s.Translate(context.Background(), "x", "hi", "fr", Glossary{}); err == nil {
		t.Error("French accepted")
	}
}

func TestSarvamRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := Sarvam{Key: "k", URL: srv.URL}.Translate(context.Background(), "x", "hi", "te", Glossary{})
	if err != ErrRateLimited {
		t.Errorf("err %v", err)
	}
}

func TestRateLimiter(t *testing.T) {
	l := NewRateLimiter(600) // one every 100 ms, bursts of 60
	start := time.Now()
	for range 60 {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Errorf("a burst of 60 took %v, want at once", d)
	}
	start = time.Now()
	for range 4 {
		l.Wait(context.Background())
	}
	if d := time.Since(start); d < 350*time.Millisecond || d > 700*time.Millisecond {
		t.Errorf("4 requests after the burst took %v, want about 400 ms", d)
	}
	// 60 a minute over a long run: never more than the budget.
	l3 := NewRateLimiter(60)
	l3.Wait(context.Background())
	if l3.slack != 5*time.Second {
		t.Errorf("slack %v, want 5 s (a burst of 6)", l3.slack)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l2 := NewRateLimiter(1)
	l2.Wait(context.Background())
	if err := l2.Wait(ctx); err == nil {
		t.Error("Wait ignored a done context")
	}
	if NewRateLimiter(0) != nil {
		t.Error("0 per minute should mean no limiter")
	}
}

func TestSarvamBatch(t *testing.T) {
	var inputs []string
	split := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		inputs = append(inputs, body["input"])
		out := strings.ReplaceAll(body["input"], "एक", "ఒకటి")
		if !split {
			out = strings.ReplaceAll(out, "\n", " ")
		}
		json.NewEncoder(w).Encode(map[string]string{"translated_text": out})
	}))
	defer srv.Close()
	s := Sarvam{Key: "k", URL: srv.URL}
	got, err := s.TranslateBatch(context.Background(), []string{"एक Dubash", "दो\nलाइन", "तीन"}, "hi", "te", Keep("Dubash"))
	if err != nil || strings.Join(got, "|") != "ఒకటి Dubash|दो लाइन|तीन" || inputs[0] != "एक [1]\nदो लाइन\nतीन" {
		t.Fatalf("got %q err %v sent %q", got, err, inputs)
	}
	split = false
	if _, err := s.TranslateBatch(context.Background(), []string{"एक", "दो"}, "hi", "te", Glossary{}); err != ErrBatchSplit {
		t.Fatalf("lost line breaks: %v", err)
	}
}

// TestApprovedTranslations: a term with an approved translation comes back
// as that translation, whichever spelling was said.
func TestApprovedTranslations(t *testing.T) {
	g := Glossary{
		Keep:  []string{"Dubash"},
		Terms: []Term{{Source: []string{"राम कथा", "रामकथा"}, Target: "రామకథ"}, {Source: []string{"चोला"}, Target: "చోళా వస్త్రం"}},
	}
	masked, restore := Mask("Dubash पर राम कथा और रामकथा, फिर चोला।", g)
	// Numbered as masked: the longest spelling first.
	if masked != "[2] पर [1] और [1], फिर [3]।" {
		t.Fatalf("masked %q", masked)
	}
	back, ok := Unmask("[2]లో [1] మరియు [1], తర్వాత [3].", restore)
	if !ok || back != "Dubashలో రామకథ మరియు రామకథ, తర్వాత చోళా వస్త్రం." {
		t.Fatalf("restored %q %v", back, ok)
	}
	p := Prompt("hi", "te", g)
	for _, want := range []string{"keep them exactly as written: Dubash", "approved translations into Telugu", "राम कथा / रामकथा → రామకథ", "Never invent names"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
}
