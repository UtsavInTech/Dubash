// Copyright 2026 the Dubash contributors. Apache-2.0.

package bench

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Hello, World!", "hello world"},
		{"We don't ship.", "we dont ship"},
		{"Rs 1,00,000 and 3.5%", "rs 100000 and 3.5"},
		{"नमस्ते, मेरा नाम लेखा है।", "नमस्ते मेरा नाम लेखा है"},
		{"२०२६ में", "2026 में"},
		{"ఈ సంవత్సరం ౨౦౨౬", "ఈ సంవత్సరం 2026"},
		{"ಕನ್ನಡ॥", "ಕನ್ನಡ"},
		{"फ़ोन और ज़रूर", "फोन और जरूर"},
	} {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWERAndCER(t *testing.T) {
	if got := WER("the cat sat", "the cat sat"); got != 0 {
		t.Errorf("identical WER = %v", got)
	}
	if got := WER("the cat sat on the mat", "the cat sit on mat"); math.Abs(got-2.0/6) > 1e-9 {
		t.Errorf("WER = %v, want 2/6", got)
	}
	// Punctuation and case never count as errors.
	if got := WER("आज हम बात करेंगे।", "आज हम बात करेंगे"); got != 0 {
		t.Errorf("danda counted as error: %v", got)
	}
	// One wrong suffix is a whole wrong word, but few wrong characters.
	ref, hyp := "ನಾವು ಹೋಗುತ್ತೇವೆ", "ನಾವು ಹೋಗುತ್ತೀವೆ"
	if w, c := WER(ref, hyp), CER(ref, hyp); w != 0.5 || c >= 0.2 {
		t.Errorf("WER %v CER %v: CER should be much smaller", w, c)
	}
}

func TestScoreTerms(t *testing.T) {
	terms := [][]string{{"Sarvam", "Sarvam AI"}, {"Kubernetes"}, {"Infosys"}}
	s := ScoreTerms("Sarvam builds for Kubernetes. Sarvam AI again.", "Sarvam builds for cuber net is. Sarvam again, Infosys.", terms)
	// Sarvam is spoken twice, once as its alias "Sarvam AI", which counts
	// once although it contains "Sarvam".
	if s.Expected != 3 || s.Found != 2 {
		t.Errorf("expected/found = %d/%d, want 3/2", s.Expected, s.Found)
	}
	if s.Inserted != 1 || !slices.Equal(s.Spurious, []string{"Infosys"}) {
		t.Errorf("inserted %d spurious %v", s.Inserted, s.Spurious)
	}
	if !slices.Contains(s.Missed, "Kubernetes") {
		t.Errorf("missed %v", s.Missed)
	}
}

func TestScoreNumbers(t *testing.T) {
	s := ScoreNumbers("Launch on 15 March with 1,200 seats, ₹499 each.", "launch on 50 March with 1200 seats 499 each")
	if s.Expected != 3 || s.Correct != 2 || !slices.Equal(s.Wrong, []string{"50"}) {
		t.Errorf("got %+v", s)
	}
	if s := ScoreNumbers("कीमत ४९९ रुपये", "कीमत 499 रुपये"); s.Correct != 1 {
		t.Errorf("Devanagari digits: %+v", s)
	}
}

func TestScoreNegations(t *testing.T) {
	if s := ScoreNegations("यह मुफ़्त नहीं है", "यह मुफ़्त है", "hi"); s.Agrees() || s.Expected != 1 || s.Found != 0 {
		t.Errorf("lost Hindi negation not caught: %+v", s)
	}
	if s := ScoreNegations("We will not raise prices.", "We will not raise prices", "en"); !s.Agrees() {
		t.Errorf("%+v", s)
	}
	// Code-switched Hindi counts English negations too.
	if s := ScoreNegations("यह free नहीं, not at all", "यह free नहीं not at all", "hi"); s.Expected != 2 || !s.Agrees() {
		t.Errorf("%+v", s)
	}
}

func TestChrF(t *testing.T) {
	if got := ChrF("नमस्ते दुनिया", "नमस्ते दुनिया"); math.Abs(got-100) > 1e-9 {
		t.Errorf("identical chrF = %v", got)
	}
	close, far := ChrF("नमस्ते दुनिया", "नमस्ते दुनिआ"), ChrF("नमस्ते दुनिया", "शुभ रात्रि")
	if !(close > far && far < 20) {
		t.Errorf("close %v far %v", close, far)
	}
}

func TestSentences(t *testing.T) {
	got := Sentences("पहला वाक्य। दूसरा? Third one. 3.5 percent stays")
	want := []string{"पहला वाक्य।", "दूसरा?", "Third one.", "3.5 percent stays"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestTranslationTerms(t *testing.T) {
	v := Vocabulary{Terms: []Term{
		{Term: "Sarvam", Keep: true},
		{Term: "keynote", Translations: map[string]string{"hi": "मुख्य भाषण"}},
		{Term: "agenda"}, // neither kept nor translated: not checked
	}}
	wanted, kept, missing := TranslationTerms("Sarvam gives the keynote on the agenda.", "Sarvam मुख्य भाषण देगा", "hi", v)
	if wanted != 2 || kept != 2 || len(missing) != 0 {
		t.Errorf("%d %d %v", wanted, kept, missing)
	}
	wanted, kept, missing = TranslationTerms("Sarvam gives the keynote.", "सर्वम मुख्य वक्तव्य देगा", "hi", v)
	if wanted != 2 || kept != 0 || len(missing) != 2 {
		t.Errorf("transliterated keep-term should be missing: %d %d %v", wanted, kept, missing)
	}
}

func tone(seconds float64, amp float64) []byte {
	n := int(seconds * SampleRate)
	s := make([]int16, n)
	for i := range s {
		s[i] = int16(amp * math.Sin(2*math.Pi*440*float64(i)/SampleRate))
	}
	return pcmBytes(s)
}

func TestWAVRoundTripAndResample(t *testing.T) {
	pcm := tone(1, 8000)
	got, err := decodeWAV(bytes.NewReader(EncodeWAV(pcm)))
	if err != nil || !bytes.Equal(got, pcm) {
		t.Fatalf("round trip: %v, %d bytes vs %d", err, len(got), len(pcm))
	}
	// 48 kHz stereo becomes 16 kHz mono of the same duration.
	frames := 48000
	stereo := make([]byte, 0, frames*4)
	for i := range frames {
		v := uint16(int16(1000 * math.Sin(float64(i))))
		stereo = binary.LittleEndian.AppendUint16(stereo, v)
		stereo = binary.LittleEndian.AppendUint16(stereo, v)
	}
	hdr := EncodeWAV(stereo)
	binary.LittleEndian.PutUint16(hdr[22:], 2)
	binary.LittleEndian.PutUint32(hdr[24:], 48000)
	out, err := decodeWAV(bytes.NewReader(hdr))
	if err != nil {
		t.Fatal(err)
	}
	if d := Seconds(out); math.Abs(d-1) > 0.01 {
		t.Errorf("duration %v, want 1 s", d)
	}
}

func TestClipsCutAtSilence(t *testing.T) {
	// 20 s loud, 0.5 s silence, 20 s loud: the cut lands in the silence.
	pcm := slices.Concat(tone(20, 8000), make([]byte, SampleRate), tone(20, 8000))
	clips := Clips(pcm, 15*time.Second, 28*time.Second)
	if len(clips) != 2 {
		t.Fatalf("%d clips", len(clips))
	}
	if d := Seconds(clips[0]); d < 20 || d > 20.5 {
		t.Errorf("first clip %.2f s, want the cut in the silence at 20–20.5 s", d)
	}
	if total := Seconds(clips[0]) + Seconds(clips[1]); math.Abs(total-Seconds(pcm)) > 1e-6 {
		t.Errorf("audio lost: %v vs %v", total, Seconds(pcm))
	}
}

func TestKeyterms(t *testing.T) {
	var vocab []string
	for i := range 55 {
		vocab = append(vocab, "term"+string(rune('a'+i%26))+strings.Repeat("x", i%3))
	}
	vocab = append([]string{strings.Repeat("y", 65)}, vocab...)
	kept, dropped := Keyterms(vocab)
	if len(kept) != 50 || len(dropped) != 6 {
		t.Errorf("kept %d dropped %d", len(kept), len(dropped))
	}
}

func TestSarvamRESTAndTranslator(t *testing.T) {
	var gotKeyterms, gotLang, gotModel string
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-subscription-key") != "k" {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/speech-to-text":
			requests++
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				t.Error(err)
			}
			gotKeyterms, gotLang, gotModel = r.FormValue("keyterms"), r.FormValue("language_code"), r.FormValue("model")
			f, _, _ := r.FormFile("file")
			data, _ := io.ReadAll(f)
			if _, err := decodeWAV(bytes.NewReader(data)); err != nil {
				t.Errorf("clip is not a valid WAV: %v", err)
			}
			json.NewEncoder(w).Encode(map[string]string{"transcript": "नमस्ते सर्वम"})
		case "/translate":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["target_language_code"] != "te-IN" || body["model"] != "sarvam-translate:v1" {
				t.Errorf("translate body %v", body)
			}
			json.NewEncoder(w).Encode(map[string]string{"translated_text": "నమస్తే"})
		}
	}))
	defer srv.Close()

	s := SarvamREST{Key: "k", URL: srv.URL}
	rec, err := s.Recognize(context.Background(), tone(40, 4000), "hi", []string{"Sarvam", "Dubash"})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || rec.Text != "नमस्ते सर्वम नमस्ते सर्वम" {
		t.Errorf("%d requests, text %q", requests, rec.Text)
	}
	if gotLang != "hi-IN" || gotModel != "saaras:v4" || gotKeyterms != `["Sarvam","Dubash"]` {
		t.Errorf("lang %q model %q keyterms %q", gotLang, gotModel, gotKeyterms)
	}
	if rec.BilledSeconds < 40 || rec.BilledSeconds > 42 {
		t.Errorf("billed %v s for 40 s of audio", rec.BilledSeconds)
	}

	out, err := SarvamTranslator{Key: "k", URL: srv.URL}.Translate(context.Background(), "नमस्ते", "hi", "te", nil)
	if err != nil || out != "నమస్తే" {
		t.Errorf("translate: %q %v", out, err)
	}
	if _, err := (SarvamTranslator{Key: "wrong", URL: srv.URL}).Translate(context.Background(), "x", "hi", "te", nil); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("bad key error: %v", err)
	}
}

// TestRunScripted runs the whole harness offline: a scripted recognizer
// that mishears one term, the fake translator, report and JSON.
func TestRunScripted(t *testing.T) {
	dir := t.TempDir()
	recDir, vocabDir := filepath.Join(dir, "recordings"), filepath.Join(dir, "vocab")
	os.MkdirAll(recDir, 0o755)
	os.MkdirAll(vocabDir, 0o755)
	os.WriteFile(filepath.Join(recDir, "demo.wav"), EncodeWAV(tone(3, 3000)), 0o644)
	os.WriteFile(filepath.Join(recDir, "demo.yaml"), []byte("lang: en\nvocabulary: demo\nreference: Welcome to Sarvam. Tickets cost 499 rupees.\ntags: [synthetic]\n"), 0o644)
	os.WriteFile(filepath.Join(vocabDir, "demo.yaml"), []byte("terms:\n  - term: Sarvam\n    keep: true\n    misheard: [Sir Vom]\n"), 0o644)

	recs, err := LoadRecordings(recDir)
	if err != nil || len(recs) != 1 {
		t.Fatal(recs, err)
	}
	opt := Options{
		Recordings: recs, VocabDir: vocabDir, Targets: []string{"hi", "en"}, Vocab: "both",
		Recognizers: []RecognizerFactory{{Name: "scripted", Provider: "scripted", New: func(r Recording, pcm []byte) Recognizer {
			return EngineRecognizer{Label: "scripted", Engine: ScriptedEngine(r.Lang, r.Reference, Seconds(pcm), map[string]string{"Sarvam.": "Sir Vom."}), Speed: 0}
		}}},
		Translators: []NamedTranslator{{Name: "fake", Provider: "fake", T: fakeKeep{}, Glossary: true}},
		Pricing: Pricing{Translate: map[string]struct {
			Per10kChars float64 `yaml:"per_10k_chars"`
		}{"fake": {Per10kChars: 20}}},
	}
	res, err := Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ASR) != 2 || len(res.MT) != 2 { // en target skipped: same as spoken
		t.Fatalf("%d asr, %d mt results", len(res.ASR), len(res.MT))
	}
	a := res.ASR[0]
	if a.Err != "" || a.Terms.Expected != 1 || a.Terms.Found != 0 || a.Misheard != 1 || a.Numbers.Correct != 1 {
		t.Errorf("asr result %+v", a)
	}
	if a.WER == 0 || len(a.Rec.Lags) != 2 {
		t.Errorf("WER %v lags %v", a.WER, a.Rec.Lags)
	}
	if m := res.MT[0]; m.TermsWanted != 1 || m.TermsKept != 1 || m.CostINR == nil || m.Sentences != 2 {
		t.Errorf("mt result %+v", m)
	}
	var md bytes.Buffer
	res.Markdown(&md)
	for _, want := range []string{"Recognition summary", "| scripted | on |", "Sir Vom", "₹"} {
		if !strings.Contains(md.String(), want) {
			t.Errorf("report lacks %q", want)
		}
	}
	if _, err := json.Marshal(res); err != nil {
		t.Error(err)
	}
}

// fakeKeep "translates" by returning the text, so keep-terms survive.
type fakeKeep struct{}

func (fakeKeep) Translate(_ context.Context, text, _, _ string, _ []string) (string, error) {
	return text, nil
}

func TestLoadRecordingsNeedsAudio(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "orphan.yaml"), []byte("lang: hi\nreference: x\n"), 0o644)
	if _, err := LoadRecordings(dir); err == nil || !strings.Contains(err.Error(), "orphan.wav") {
		t.Errorf("got %v", err)
	}
}
