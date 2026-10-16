package asr

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultWhisperModel is the model name the OpenAI API uses for Whisper.
// Servers that load a single model (whisper.cpp) ignore it.
const DefaultWhisperModel = "whisper-1"

// Whisper transcribes with any server that implements the OpenAI audio
// transcription API (POST {URL}/audio/transcriptions): whisper.cpp's
// server, faster-whisper servers such as Speaches, LocalAI, or a hosted
// provider. With a server on the venue's network, captions need no
// Internet connection at all.
//
// The API transcribes whole files, not streams, so Whisper cuts the audio
// into utterances at the speaker's pauses (or at the quietest moment of a
// long run) and sends each one as a WAV file. Utterances are transcribed
// one at a time, in order, while audio keeps being read. With Interim set,
// the utterance in progress is also transcribed every Interim while the
// server has nothing else to do, for a live line in the original language.
type Whisper struct {
	URL    string       // base URL, such as http://localhost:8000/v1
	Model  string       // DefaultWhisperModel if empty
	Key    string       // bearer token, if the server wants one
	Client *http.Client // http.DefaultClient if nil

	// Pause ends an utterance (default 600 ms); MaxUtterance bounds one
	// (default 10 s); Interim is how often to transcribe the utterance in
	// progress (0 disables interims).
	Pause        time.Duration
	MaxUtterance time.Duration
	Interim      time.Duration
}

// Utterances with less speech than this are noise (a cough, a click).
const minSpeech = 250 * time.Millisecond

// preroll keeps the audio just before speech starts, so the first
// syllable is not cut.
const preroll = 300 * time.Millisecond

// requestTimeout bounds one transcription request.
const requestTimeout = 30 * time.Second

// maxPrompt bounds the vocabulary prompt: Whisper reads only the last 224
// tokens of it.
const maxPrompt = 600

// utterance is audio to transcribe: seq numbers utterances within a Run,
// and at is where the utterance ends on the event clock.
type utterance struct {
	pcm []byte
	seq int64
	at  float64
}

// Run implements Engine. It returns an error only when the server rejects
// the requests outright (bad URL, key or model).
func (w *Whisper) Run(ctx context.Context, cfg Config, audio <-chan []byte, events chan<- Event) error {
	seg := segmenter{
		pause:     bytesOf(cmp.Or(w.Pause, 600*time.Millisecond)),
		maxLen:    bytesOf(cmp.Or(w.MaxUtterance, 10*time.Second)),
		preroll:   bytesOf(preroll),
		minSpeech: bytesOf(minSpeech),
	}
	finals := make(chan utterance, 64)
	var latest atomic.Pointer[utterance] // utterance in progress, for an interim
	var ended atomic.Int64               // utterances cut so far
	wake := make(chan struct{}, 1)
	workErr := make(chan error, 1)
	go func() { workErr <- w.work(ctx, cfg, finals, &latest, &ended, wake, events) }()

	var total, lastInterim int64
	queue := func(pcm []byte) {
		n := ended.Add(1)
		u := utterance{pcm: pcm, seq: n - 1, at: float64(total-int64(seg.size)) / BytesPerSecond}
		select {
		case finals <- u:
		default:
			send(ctx, events, Event{Kind: Error, Text: "transcription is falling behind: an utterance was dropped", At: u.at})
		}
	}
	for {
		select {
		case <-ctx.Done():
			close(finals)
			<-workErr
			return nil
		case err := <-workErr:
			return err
		case chunk, ok := <-audio:
			if !ok {
				if pcm := seg.flush(); pcm != nil {
					queue(pcm)
				}
				close(finals)
				return <-workErr
			}
			total += int64(len(chunk))
			if pcm := seg.push(chunk); pcm != nil {
				queue(pcm)
				lastInterim = total
			}
			if w.Interim > 0 && seg.speech >= bytesOf(time.Second) && total-lastInterim >= int64(bytesOf(w.Interim)) {
				lastInterim = total
				latest.Store(&utterance{pcm: seg.audio(), seq: ended.Load(), at: float64(total) / BytesPerSecond})
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}
	}
}

// work transcribes utterances until finals is closed or ctx is done:
// finals first, in order, and the latest interim when there is none.
func (w *Whisper) work(ctx context.Context, cfg Config, finals <-chan utterance, latest *atomic.Pointer[utterance], ended *atomic.Int64, wake <-chan struct{}, events chan<- Event) error {
	final := func(u utterance) error {
		text, err := w.transcribe(ctx, cfg, u.pcm)
		if err != nil && !fatal(err) && sleep(ctx, 500*time.Millisecond) {
			text, err = w.transcribe(ctx, cfg, u.pcm) // one retry for a blip
		}
		switch {
		case ctx.Err() != nil:
			return nil
		case fatal(err):
			return err
		case err != nil:
			send(ctx, events, Event{Kind: Error, Text: err.Error(), At: u.at})
		case text != "":
			send(ctx, events, Event{Kind: Final, Text: text, At: u.at})
		}
		return nil
	}
	for {
		select { // finals never wait behind an interim
		case u, ok := <-finals:
			if !ok {
				return nil
			}
			if err := final(u); err != nil {
				return err
			}
			continue
		default:
		}
		select {
		case u, ok := <-finals:
			if !ok {
				return nil
			}
			if err := final(u); err != nil {
				return err
			}
		case <-wake:
			u := latest.Swap(nil)
			if u == nil || u.seq != ended.Load() {
				continue // that utterance has ended: its final is queued
			}
			text, err := w.transcribe(ctx, cfg, u.pcm)
			if fatal(err) {
				return err
			}
			if err == nil && text != "" && u.seq == ended.Load() {
				send(ctx, events, Event{Kind: Interim, Text: text, At: u.at})
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// statusError is a non-2xx answer from the transcription server.
type statusError struct {
	code int
	msg  string
}

// Error implements error.
func (e *statusError) Error() string {
	return fmt.Sprintf("transcription server: %d %s", e.code, e.msg)
}

// fatal reports an error that retrying cannot fix: the server rejected the
// request itself (wrong URL, key or model), not the moment.
func fatal(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.code >= 400 && se.code < 500 &&
		se.code != http.StatusRequestTimeout && se.code != http.StatusTooManyRequests
}

// transcribe sends one utterance and returns its text on a single line.
func (w *Whisper) transcribe(ctx context.Context, cfg Config, pcm []byte) (string, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="audio.wav"`)
	h.Set("Content-Type", "audio/wav")
	part, err := form.CreatePart(h)
	if err != nil {
		return "", err
	}
	writeWAV(part, pcm)
	fields := map[string]string{
		"model":           cmp.Or(w.Model, DefaultWhisperModel),
		"response_format": "json",
		"temperature":     "0",
	}
	if cfg.Lang != "" {
		fields["language"] = cfg.Lang
	}
	if p := vocabularyPrompt(cfg.Vocabulary); p != "" {
		fields["prompt"] = p
	}
	for k, v := range fields {
		form.WriteField(k, v)
	}
	if err := form.Close(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(w.URL, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return "", &statusError{code: http.StatusBadRequest, msg: err.Error()}
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	if w.Key != "" {
		req.Header.Set("Authorization", "Bearer "+w.Key)
	}
	client := w.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("transcription server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", &statusError{code: resp.StatusCode, msg: strings.TrimSpace(string(msg))}
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("transcription server: decode response: %w", err)
	}
	return strings.Join(strings.Fields(out.Text), " "), nil
}

// vocabularyPrompt lists the glossary as Whisper's prompt, which biases
// the spelling of names and jargon.
func vocabularyPrompt(terms []string) string {
	p := strings.Join(terms, ", ")
	if len(p) > maxPrompt {
		p = p[:strings.LastIndex(p[:maxPrompt], ", ")]
	}
	return p
}

// writeWAV writes pcm (16 kHz mono PCM16) as a WAV file.
func writeWAV(w io.Writer, pcm []byte) {
	const rate, bits, channels = 16000, 16, 1
	hdr := struct {
		Riff     [4]byte
		Size     uint32
		Wave     [4]byte
		Fmt      [4]byte
		FmtSize  uint32
		Format   uint16
		Channels uint16
		Rate     uint32
		ByteRate uint32
		Align    uint16
		Bits     uint16
		Data     [4]byte
		DataSize uint32
	}{
		[4]byte{'R', 'I', 'F', 'F'}, uint32(36 + len(pcm)), [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, 16, 1, channels, rate, rate * channels * bits / 8, channels * bits / 8, bits,
		[4]byte{'d', 'a', 't', 'a'}, uint32(len(pcm)),
	}
	binary.Write(w, binary.LittleEndian, hdr)
	w.Write(pcm)
}

// segmenter cuts a stream of audio chunks into utterances with an energy
// voice detector. Sizes are in bytes of wire audio.
type segmenter struct {
	pause, maxLen, preroll, minSpeech int

	chunks   [][]byte
	voiced   []bool
	size     int  // bytes held
	speaking bool // an utterance is open
	speech   int  // voiced bytes in the open utterance
	quiet    int  // unvoiced bytes since the last voiced chunk
}

// push adds a chunk and returns the utterance it closes, if any.
func (s *segmenter) push(chunk []byte) []byte {
	v := RMS(chunk) >= voicedRMS
	s.chunks = append(s.chunks, chunk)
	s.voiced = append(s.voiced, v)
	s.size += len(chunk)
	if !s.speaking {
		if !v {
			for len(s.chunks) > 1 && s.size-len(s.chunks[0]) >= s.preroll {
				s.drop(1)
			}
			return nil
		}
		s.speaking, s.speech, s.quiet = true, 0, 0
	}
	if v {
		s.speech += len(chunk)
		s.quiet = 0
	} else {
		s.quiet += len(chunk)
	}
	switch {
	case s.quiet >= s.pause:
		return s.cut(len(s.chunks))
	case s.size >= s.maxLen:
		return s.cut(s.quietest())
	}
	return nil
}

// flush closes the open utterance, if any.
func (s *segmenter) flush() []byte {
	if !s.speaking {
		return nil
	}
	return s.cut(len(s.chunks))
}

// audio returns the audio held, in one slice.
func (s *segmenter) audio() []byte {
	return bytes.Join(s.chunks, nil)
}

// quietest returns where to cut a long utterance: after the quietest
// chunk of its last third, so words are not split.
func (s *segmenter) quietest() int {
	n := len(s.chunks)
	best, bestLevel := n, -1.0
	for i := n * 2 / 3; i < n-1; i++ {
		if l := RMS(s.chunks[i]); bestLevel < 0 || l < bestLevel {
			best, bestLevel = i+1, l
		}
	}
	return best
}

// cut closes the utterance after the first n chunks; the rest opens the
// next one. It returns nil if the utterance held too little speech.
func (s *segmenter) cut(n int) []byte {
	pcm := bytes.Join(s.chunks[:n], nil)
	speech := 0
	for i := range n {
		if s.voiced[i] {
			speech += len(s.chunks[i])
		}
	}
	s.drop(n)
	s.speech, s.quiet = 0, 0
	for i, c := range s.chunks {
		if s.voiced[i] {
			s.speech += len(c)
			s.quiet = 0
		} else {
			s.quiet += len(c)
		}
	}
	s.speaking = s.speech > 0
	if speech < s.minSpeech {
		return nil
	}
	return pcm
}

func (s *segmenter) drop(n int) {
	for _, c := range s.chunks[:n] {
		s.size -= len(c)
	}
	s.chunks = s.chunks[n:]
	s.voiced = s.voiced[n:]
}

// sleep waits d and reports whether ctx is still alive.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func bytesOf(d time.Duration) int {
	return int(d * BytesPerSecond / time.Second)
}
