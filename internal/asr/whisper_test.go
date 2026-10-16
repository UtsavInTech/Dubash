package asr

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// tone is 100 ms of a loud square wave; silence is 100 ms of nothing.
func tone() []byte {
	b := make([]byte, BytesPerSecond/10)
	for i := 0; i < len(b); i += 4 {
		binary.LittleEndian.PutUint16(b[i:], uint16(8000))
		binary.LittleEndian.PutUint16(b[i+2:], uint16(0xffff-8000+1)) // -8000
	}
	return b
}

func silence() []byte { return make([]byte, BytesPerSecond/10) }

// whisperServer is a fake transcription server. It checks each request
// and answers with the seconds of audio it got, like "2.0s".
type whisperServer struct {
	t      *testing.T
	status int // non-zero: answer every request with it
	mu     sync.Mutex
	forms  []map[string]string
}

func (ws *whisperServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if ws.status != 0 {
		http.Error(w, `{"error":"nope"}`, ws.status)
		return
	}
	if r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer k" {
		http.Error(w, "wrong request", http.StatusNotFound)
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wav, _ := io.ReadAll(f)
	form := map[string]string{"filename": hdr.Filename, "type": hdr.Header.Get("Content-Type")}
	for k, v := range r.MultipartForm.Value {
		form[k] = v[0]
	}
	ws.mu.Lock()
	ws.forms = append(ws.forms, form)
	ws.mu.Unlock()
	if string(wav[:4]) != "RIFF" || string(wav[8:16]) != "WAVEfmt " ||
		binary.LittleEndian.Uint32(wav[24:]) != 16000 || int(binary.LittleEndian.Uint32(wav[40:])) != len(wav)-44 {
		http.Error(w, "bad wav", http.StatusBadRequest)
		return
	}
	secs := float64(len(wav)-44) / BytesPerSecond
	json.NewEncoder(w).Encode(map[string]string{"text": fmt.Sprintf(" %.1fs\n", secs)})
}

func runWhisper(t *testing.T, w *Whisper, chunks [][]byte) ([]Event, error) {
	t.Helper()
	audio := make(chan []byte)
	events := make(chan Event, 64)
	go func() {
		for _, c := range chunks {
			audio <- c
		}
		close(audio)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := w.Run(ctx, Config{Lang: "es", Vocabulary: []string{"Nerdearla", "eBPF"}}, audio, events)
	close(events)
	var out []Event
	for ev := range events {
		out = append(out, ev)
	}
	return out, err
}

func repeat(c func() []byte, n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = c()
	}
	return out
}

func TestWhisperCutsAtPauses(t *testing.T) {
	ws := &whisperServer{t: t}
	srv := httptest.NewServer(ws)
	defer srv.Close()

	var chunks [][]byte
	chunks = append(chunks, repeat(silence, 5)...) // 0.3 s kept as preroll
	chunks = append(chunks, repeat(tone, 10)...)   // 1 s of speech
	chunks = append(chunks, repeat(silence, 8)...) // a pause ends it
	chunks = append(chunks, repeat(tone, 2)...)    // a cough: too short
	chunks = append(chunks, repeat(silence, 8)...)
	chunks = append(chunks, repeat(tone, 15)...) // 1.5 s, cut when audio ends
	events, err := runWhisper(t, &Whisper{URL: srv.URL + "/v1/", Key: "k"}, chunks)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ev := range events {
		got = append(got, fmt.Sprintf("%d %s @%.1f", ev.Kind, ev.Text, ev.At))
	}
	// Preroll 0.3 + speech 1 + pause 0.6 = 1.9 s, ending at 2.1 s. The
	// cough is dropped; the last utterance keeps the 0.2 s of silence
	// before it.
	want := []string{"1 1.9s @2.1", "1 1.7s @4.8"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %q, want %q", got, want)
	}
	f := ws.forms[0]
	if f["filename"] != "audio.wav" || f["type"] != "audio/wav" || f["model"] != DefaultWhisperModel ||
		f["language"] != "es" || f["prompt"] != "Nerdearla, eBPF" || f["response_format"] != "json" {
		t.Fatalf("form = %v", f)
	}
}

func TestWhisperCutsLongRunsAtTheQuietestMoment(t *testing.T) {
	ws := &whisperServer{t: t}
	srv := httptest.NewServer(ws)
	defer srv.Close()

	// 2.5 s of speech with one quiet chunk at 1.6 s, and a 2 s limit.
	chunks := repeat(tone, 25)
	chunks[16] = silence()
	events, err := runWhisper(t, &Whisper{URL: srv.URL + "/v1", Key: "k", MaxUtterance: 2 * time.Second}, chunks)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Text != "1.7s" || events[1].Text != "0.8s" {
		t.Fatalf("events = %+v", events)
	}
}

func TestWhisperInterims(t *testing.T) {
	ws := &whisperServer{t: t}
	srv := httptest.NewServer(ws)
	defer srv.Close()

	audio := make(chan []byte)
	events := make(chan Event, 64)
	done := make(chan error, 1)
	go func() {
		done <- (&Whisper{URL: srv.URL + "/v1", Key: "k", Interim: 500 * time.Millisecond}).Run(context.Background(), Config{Lang: "en"}, audio, events)
	}()
	for _, c := range repeat(tone, 12) {
		audio <- c
	}
	select {
	case ev := <-events:
		if ev.Kind != Interim || ev.Text != "1.0s" {
			t.Fatalf("first event = %+v, want an interim once 1 s was spoken", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no interim while speaking")
	}
	close(audio)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if ev := <-events; ev.Kind != Final || ev.Text != "1.2s" {
		t.Fatalf("last event = %+v, want the final", ev)
	}
}

func TestWhisperStopsWhenTheServerRejectsTheRequest(t *testing.T) {
	srv := httptest.NewServer(&whisperServer{t: t, status: http.StatusUnauthorized})
	defer srv.Close()
	_, err := runWhisper(t, &Whisper{URL: srv.URL + "/v1"}, repeat(tone, 10))
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want the 401", err)
	}
}

func TestWhisperKeepsGoingAfterAServerError(t *testing.T) {
	srv := httptest.NewServer(&whisperServer{t: t, status: http.StatusInternalServerError})
	defer srv.Close()
	events, err := runWhisper(t, &Whisper{URL: srv.URL + "/v1"}, repeat(tone, 10))
	if err != nil || len(events) != 1 || events[0].Kind != Error || !strings.Contains(events[0].Text, "500") {
		t.Fatalf("events = %+v, err = %v", events, err)
	}
}

func TestVocabularyPromptIsBounded(t *testing.T) {
	terms := make([]string, 200)
	for i := range terms {
		terms[i] = fmt.Sprintf("term%03d", i)
	}
	p := vocabularyPrompt(terms)
	if len(p) > maxPrompt || !strings.HasPrefix(p, "term000, term001") || strings.HasSuffix(p, ", ") {
		t.Fatalf("prompt = %q (%d bytes)", p, len(p))
	}
}
