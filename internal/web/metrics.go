package web

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/UtsavInTech/dubash/internal/room"
)

// metric is one Prometheus metric family, read from a room's status.
type metric struct {
	name, kind, help string
	value            func(room.Status) float64
}

var metrics = []metric{
	{"dubash_room_live", "gauge", "Whether an operator is streaming audio into the room (1) or not (0).",
		func(s room.Status) float64 { return b2f(s.Live) }},
	{"dubash_room_viewers", "gauge", "Viewers connected to the room's captions.",
		func(s room.Status) float64 { return float64(s.Viewers) }},
	{"dubash_room_talk", "gauge", "Number of the room's current talk.",
		func(s room.Status) float64 { return float64(s.Talk) }},
	{"dubash_room_segments_total", "counter", "Caption lines published since the server started.",
		func(s room.Status) float64 { return float64(s.Segments) }},
	{"dubash_room_errors_total", "counter", "Transcription, translation and storage errors since the server started.",
		func(s room.Status) float64 { return float64(s.Errors) }},
	{"dubash_room_translation_latency_seconds", "gauge", "Mean time from a finished sentence to its translated line being published.",
		func(s room.Status) float64 { return float64(s.AvgLatencyMs) / 1000 }},
	{"dubash_room_audio_level_dbfs", "gauge", "Loudest input level of the last second, in dBFS (-99 is silence).",
		func(s room.Status) float64 { return s.AudioLevel }},
	{"dubash_room_audio_seconds", "gauge", "Seconds of audio received in the current or last transmission.",
		func(s room.Status) float64 { return s.AudioSeconds }},
	{"dubash_room_last_audio_timestamp_seconds", "gauge", "Unix time audio last arrived (0 if never).",
		func(s room.Status) float64 { return unix(s.LastAudioAt) }},
	{"dubash_room_last_text_timestamp_seconds", "gauge", "Unix time the recognizer last produced text (0 if never).",
		func(s room.Status) float64 { return unix(s.LastTextAt) }},
}

// metrics serves every room's status in the Prometheus text format, for
// alerting on a room that goes silent or starts failing mid-event.
func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	statuses := make([]room.Status, len(s.order))
	for i, rm := range s.order {
		statuses[i] = rm.Status()
	}
	var b bytes.Buffer
	for _, m := range metrics {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", m.name, m.help, m.name, m.kind)
		for _, st := range statuses {
			// Room ids are [a-z0-9-]: nothing to escape.
			fmt.Fprintf(&b, "%s{room=%q} %g\n", m.name, st.ID, m.value(st))
		}
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write(b.Bytes())
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func unix(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixMilli()) / 1000
}
