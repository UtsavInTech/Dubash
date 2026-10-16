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

func TestSarvamChat(t *testing.T) {
	var sys, user string
	reply := "1. ఒకటి\n2) రెండు Dubash\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model           string              `json:"model"`
			ReasoningEffort *string             `json:"reasoning_effort"`
			Messages        []map[string]string `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "sarvam-105b" || body.ReasoningEffort != nil || r.Header.Get("api-subscription-key") != "k" {
			t.Errorf("request %+v", body)
		}
		sys, user = body.Messages[0]["content"], body.Messages[1]["content"]
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": reply}}},
			"usage":   map[string]int{"prompt_tokens": 120, "completion_tokens": 20},
		})
	}))
	defer srv.Close()
	m := &Meter{}
	s := SarvamChat{Key: "k", URL: srv.URL, Meter: m}
	got, err := s.TranslateBatch(context.Background(), []string{"एक", "दो  Dubash"}, "hi", "te",
		Glossary{Keep: []string{"Dubash"}, Terms: []Term{{Source: []string{"राम कथा"}, Target: "రామకథ"}}})
	if err != nil || strings.Join(got, "|") != "ఒకటి|రెండు Dubash" {
		t.Fatalf("got %q %v", got, err)
	}
	if user != "2 lines; answer with exactly 2 numbered lines.\n1. एक\n2. दो Dubash\n" || !strings.Contains(sys, "from Hindi into Telugu") || !strings.Contains(sys, "राम कथा → రామకథ") || !strings.Contains(sys, "never invent names") {
		t.Errorf("prompt:\n%s\n%s", sys, user)
	}
	if m.PromptTokens.Load() != 120 || m.CompletionTokens.Load() != 20 || m.Requests.Load() != 1 {
		t.Errorf("meter %d %d", m.PromptTokens.Load(), m.CompletionTokens.Load())
	}
	reply = "1. ఒకటి\n" // a line went missing
	if _, err := s.TranslateBatch(context.Background(), []string{"एक", "दो"}, "hi", "te", Glossary{}); err != ErrBatchSplit {
		t.Fatalf("missing line: %v", err)
	}
	reply = "1. ఒకటి\n"
	if out, err := s.Translate(context.Background(), "एक", "hi", "te", Glossary{}); err != nil || out != "ఒకటి" {
		t.Fatalf("single: %q %v", out, err)
	}
}

func TestParseNumbered(t *testing.T) {
	for _, c := range []struct {
		out  string
		n    int
		want string
		ok   bool
	}{
		{"1. a\n2. b", 2, "a|b", true},
		{"2. b\n1) a\n", 2, "a|b", true},
		{"a\nb", 2, "a|b", true},                    // numbers dropped
		{"Then he said.", 1, "Then he said.", true}, // a single line, unnumbered
		{"1. a", 2, "", false},
		{"a\nb\nc", 2, "", false},
	} {
		got, ok := parseNumbered(c.out, c.n)
		if ok != c.ok || ok && strings.Join(got, "|") != c.want {
			t.Errorf("parseNumbered(%q, %d) = %q %v", c.out, c.n, got, ok)
		}
	}
}

func TestTransliterated(t *testing.T) {
	hi := "हनुमान जी बोले एक तरफ हो जाओ शर्त में तुम ना फसो बीच में।"
	for _, c := range []struct {
		out  string
		want bool
	}{
		{"హనుమాన్ జీ బోలే, ఏక్ తీర్ హో జావో, శరత్ మే తుమ్ నా ఫసో, బీచ్ మే.", true},         // Telugu script, Hindi words
		{"ಹನುಮಾನ್ ಜಿ ಭೋಲೇ ಏಕ್ ತೀಂತ್ ಹೋ ಜಾವೋ ಶರತ್ ಮೇ ತುಮ್ ನಾ ಫಸೋ ಬೀಚ್ ಮೇ.", true},           // Kannada script, Hindi words
		{"హనుమాన్ జీ అన్నారు, ఒక పక్కకు తప్పుకోండి, మీరు పందెంలో ఇరుక్కోకండి.", false},     // translated
		{"ಹನುಮಂತರು, \"ಒಂದು ಕಡೆ ಸರಿಯಿರಿ, ಪಣಕ್ಕೆ ಬೀಳಬೇಡಿ, ಮಧ್ಯದಲ್ಲಿ ಇರಬೇಡಿ\" ಎಂದರು.", false}, // translated
		{"Hanuman ji said, step aside.", false},
	} {
		if got := Transliterated(hi, c.out); got != c.want {
			t.Errorf("Transliterated(…, %q) = %v", c.out, got)
		}
	}
	if Transliterated("डॉ अनन्या राव", "డాక్టర్ అనన్య రావు") {
		t.Error("a short line of names was judged")
	}
}

// TestSarvamChatRetriesTransliteration: a line only written out in the
// target script is retried with context, then given to the fallback.
func TestSarvamChatRetriesTransliteration(t *testing.T) {
	hi := "हनुमान जी बोले एक तरफ हो जाओ"
	translit := "హనుమాన్ జీ బోలే ఏక్ తరఫ్ హో జావో"
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": "1. " + translit}}}})
	}))
	defer srv.Close()
	fb := &countingTr{out: "హనుమాన్ అన్నారు, పక్కకు జరుగు"}
	s := SarvamChat{Key: "k", URL: srv.URL, Fallback: fb}
	out, err := s.Translate(context.Background(), hi, "hi", "te", Glossary{})
	if err != nil || out != fb.out || calls != 2 || fb.n != 1 {
		t.Fatalf("out %q err %v, %d model calls, %d fallback calls", out, err, calls, fb.n)
	}
}

type countingTr struct {
	out string
	n   int
}

func (c *countingTr) Translate(context.Context, string, string, string, Glossary) (string, error) {
	c.n++
	return c.out, nil
}

func TestGarbled(t *testing.T) {
	hi := "सोई ब्रह्मा जी प्रकट हो गए रुकियो रुकियो रुकियो।"
	if !Garbled(hi, "సోయ్ బ్రహ్మా జీ ప్రకాష్ హో గయే రukiyో రukiyో రukiyో.", "te") {
		t.Error("half-Latin words not caught")
	}
	if Garbled(hi, "అప్పుడు బ్రహ్మ ప్రత్యక్షమై ఆగు ఆగు ఆగు అన్నారు.", "te") {
		t.Error("a translation was rejected")
	}
	if Garbled("हमारी team ने cost बचाई", "మా team cost ఆదా చేసింది", "te") {
		t.Error("English words of a code-mixed line are fine")
	}
}

func TestLooping(t *testing.T) {
	if !Garbled("बा जब रावण ने मारा था तब नहीं तुम प्रकट भए।", "రావణుడి చేతిలో బ్రహ్మదేవుడి చేతిలో బ్రహ్మదేవుడి చేతిలో బ్రహ్మదేవుడి చేతిలో బ్రహ్మదేవుడి చేతిలో", "te") {
		t.Error("a loop was not caught")
	}
	if Garbled("रुको तो रुको तो रुको तो", "ఆగు ఆగు ఆగు అన్నారు", "te") {
		t.Error("a repetition the speaker made is fine")
	}
}
