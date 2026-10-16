package web

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UtsavInTech/dubash/internal/speaker"
)

func speakerRequest(t *testing.T, server *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	r, e := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Authorization", "Bearer secret")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	return res
}
func TestSpeakerConsentRules(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	res := speakerRequest(
		t, ts, "POST", "/api/admin/speakers",
		`{"displayName":"Test Voice","languages":["hi"],"style":"lecture"}`,
	)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("profile status %d", res.StatusCode)
	}
	var p speaker.Profile
	if e := json.NewDecoder(res.Body).Decode(&p); e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	res = speakerRequest(t, ts, "POST", "/api/admin/speakers/"+p.ID+"/samples", "")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("upload without consent status %d", res.StatusCode)
	}
	res.Body.Close()
	res = speakerRequest(
		t, ts, "POST", "/api/admin/speakers/"+p.ID+"/consents",
		`{"permission":"recognition","granted":true,"by":"organiser"}`,
	)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing written permission status %d", res.StatusCode)
	}
	res.Body.Close()
	res = speakerRequest(
		t, ts, "POST", "/api/admin/speakers/"+p.ID+"/consents",
		`{"permission":"synthetic_voice","granted":true,"by":"speaker"}`,
	)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("synthetic consent status %d", res.StatusCode)
	}
	res.Body.Close()
	res = speakerRequest(
		t, ts, "POST", "/api/admin/speakers/"+p.ID+"/consents",
		`{"permission":"storage","granted":true,"by":"speaker","retentionDays":14}`,
	)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad retention status %d", res.StatusCode)
	}
	res.Body.Close()
	res = speakerRequest(t, ts, "GET", "/api/admin/speakers", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("authenticated listing status %d", res.StatusCode)
	}
	res.Body.Close()
	anon, e := http.Get(ts.URL + "/api/admin/speakers")
	if e != nil {
		t.Fatal(e)
	}
	defer anon.Body.Close()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status %d", anon.StatusCode)
	}
}
func TestSpeakerSignalChecksAndDurationLimits(t *testing.T) {
	pcm := make([]byte, 32000)
	for i := 0; i < 16000; i++ {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(1000)))
	}
	x := analysePCM(pcm, 1)
	if !x.Analysed || x.DurationSeconds != 1 || x.PeakDBFS == nil || x.RMSDBFS == nil || x.NoiseDBFS == nil {
		t.Fatalf("checks=%+v", x)
	}
	clipped := make([]byte, 32000)
	for i := 0; i < 16000; i++ {
		binary.LittleEndian.PutUint16(clipped[i*2:], uint16(int16(32767)))
	}
	if !analysePCM(clipped, 1).Clipping {
		t.Fatal("expected clipping above -0.5 dBFS")
	}
	if _, e := encodedDuration(bytes.Repeat([]byte{0}, 100), ".mp3"); e == nil {
		t.Fatal("accepted MP3 without frames")
	}
	frame := make([]byte, 417*2300)
	for i := 0; i < 2300; i++ {
		off := i * 417
		frame[off] = 0xff
		frame[off+1] = 0xfb
		frame[off+2] = 0x90
		frame[off+3] = 0
	}
	seconds, e := encodedDuration(frame, ".mp3")
	if e != nil || seconds < 59 || seconds > 61 {
		t.Fatalf("MP3 duration %.2f err=%v", seconds, e)
	}
	m4a := make([]byte, 40)
	copy(m4a[4:], "mvhd")
	binary.BigEndian.PutUint32(m4a[20:], 1000)
	binary.BigEndian.PutUint32(m4a[24:], 60000)
	seconds, e = encodedDuration(m4a, ".m4a")
	if e != nil || seconds != 60 {
		t.Fatalf("M4A duration %.2f err=%v", seconds, e)
	}
}

func TestSpeakerRetentionRemovesExpiredFiles(t *testing.T) {
	store, err := speaker.OpenFile(filepath.Join(t.TempDir(), "speakers.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	path := filepath.Join(t.TempDir(), "expired.wav")
	if err = os.WriteFile(path, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = store.SaveProfile(context.Background(), speaker.Profile{
		ID: "retention-speaker", DisplayName: "Retention",
		Languages: []string{"hi"}, Style: "lecture",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AddSample(context.Background(), speaker.Sample{
		ProfileID: "retention-speaker", FileName: "expired.wav", FilePath: path,
		UploadedAt: now.Add(-48 * time.Hour), DeleteAfter: now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	site := &Server{Speakers: store, log: slog.New(slog.DiscardHandler)}
	site.retainOnce(context.Background(), now)
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired file remains, err=%v", err)
	}
	xs, err := store.Samples(context.Background(), "retention-speaker")
	if err != nil || len(xs) != 0 {
		t.Fatalf("expired sample metadata=%+v err=%v", xs, err)
	}
}

func TestSpeakerSampleDurationAndTotalLimits(t *testing.T) {
	for _, duration := range []float64{59.9, 900.1} {
		if err := validateSampleLimits(duration, nil); err == nil {
			t.Errorf("accepted duration %.1f", duration)
		}
	}
	if err := validateSampleLimits(60, nil); err != nil {
		t.Errorf("rejected minimum duration: %v", err)
	}
	if err := validateSampleLimits(900, nil); err != nil {
		t.Errorf("rejected maximum duration: %v", err)
	}
	if err := validateSampleLimits(900, []speaker.Sample{{DurationSeconds: 900}}); err != nil {
		t.Errorf("rejected 30 minute total: %v", err)
	}
	if err := validateSampleLimits(900, []speaker.Sample{{DurationSeconds: 900}, {DurationSeconds: 1}}); err == nil {
		t.Fatal("accepted more than 30 minutes total")
	}
	if !validSampleSize(maxSpeakerUpload) || validSampleSize(maxSpeakerUpload+1) {
		t.Fatal("60 MB per-file boundary is wrong")
	}
	for _, days := range []int{30, 90, 365} {
		if !validStorageDays(days) {
			t.Errorf("rejected %d-day retention", days)
		}
	}
	if validStorageDays(14) {
		t.Fatal("accepted invalid retention period")
	}
}

func TestRecognitionWithdrawalDeletesSamplesAndRetiresSpeakerWords(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	res := speakerRequest(
		t, ts, "POST", "/api/admin/speakers",
		`{"displayName":"Withdrawal Test","languages":["hi"],"style":"lecture"}`,
	)
	var p speaker.Profile
	if e := json.NewDecoder(res.Body).Decode(&p); e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	for _, permission := range []string{"recognition", "storage", "terms"} {
		body := `{"permission":"` + permission + `","granted":true,"by":"speaker","retentionDays":30}`
		res = speakerRequest(t, ts, "POST", "/api/admin/speakers/"+p.ID+"/consents", body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("grant %s: %d", permission, res.StatusCode)
		}
		res.Body.Close()
	}
	res = speakerRequest(
		t, ts, "POST", "/api/admin/speakers/"+p.ID+"/words",
		`{"term":"my phrase","meaning":"test phrase","lang":"hi"}`,
	)
	var entry struct {
		ID string `json:"id"`
	}
	if e := json.NewDecoder(res.Body).Decode(&entry); e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if entry.ID == "" {
		t.Fatal("speaker phrase did not return a bank entry id")
	}
	wav := make([]byte, 44+16000*60*2)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 16000)
	binary.LittleEndian.PutUint32(wav[28:], 32000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(wav)-44))
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, e := mw.CreateFormFile("file", "one-minute.wav")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = part.Write(wav); e != nil {
		t.Fatal(e)
	}
	if e = mw.Close(); e != nil {
		t.Fatal(e)
	}
	req, e := http.NewRequest("POST", ts.URL+"/api/admin/speakers/"+p.ID+"/samples", &form)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("sample upload: %d %s", res.StatusCode, b)
	}
	res.Body.Close()
	res = speakerRequest(t, ts, "POST", "/api/admin/speakers/"+p.ID+"/consents/recognition/withdraw", `{"by":"speaker"}`)
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("withdrawal: %d %s", res.StatusCode, b)
	}
	res.Body.Close()
	res = speakerRequest(t, ts, "GET", "/api/admin/speakers/"+p.ID+"/samples", "")
	var samples []speaker.Sample
	if e := json.NewDecoder(res.Body).Decode(&samples); e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if len(samples) != 0 {
		t.Fatalf("withdrawal left samples: %+v", samples)
	}
	res = speakerRequest(t, ts, "GET", "/api/admin/bank/entries?scope=speaker&scopeId="+p.ID, "")
	var entries []map[string]any
	if e := json.NewDecoder(res.Body).Decode(&entries); e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	found := false
	for _, x := range entries {
		if x["id"] == entry.ID {
			found = true
			if x["status"] != "retired" {
				t.Fatalf("withdrawn phrase status=%v", x["status"])
			}
		}
	}
	if !found {
		t.Fatal("speaker bank phrase missing after recognition withdrawal")
	}
}
