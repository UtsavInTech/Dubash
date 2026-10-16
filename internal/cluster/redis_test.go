// Copyright 2026 the Dubash contributors. Apache-2.0.

package cluster

import (
	"bufio"
	"strings"
	"testing"
)

func TestRESP(t *testing.T) {
	for _, tt := range []struct {
		in, want string
		bad      bool
	}{
		{"+OK\r\n", "OK", false}, {":1\r\n", "1", false}, {"$5\r\nhello\r\n", "hello", false},
		{"$-1\r\n", "", false}, {"$0\r\n\r\n", "", false}, {"-ERR injected\r\n", "", true},
		{"$10000000\r\n", "", true}, {"$-2\r\n", "", true}, {"$3\r\nab", "", true}, {"+OK\n", "", true},
	} {
		out, err := readRESP(bufio.NewReader(strings.NewReader(tt.in)))
		if (err != nil) != tt.bad || (!tt.bad && out != tt.want) {
			t.Errorf("%q: %q %v", tt.in, out, err)
		}
	}
}
func TestRoomRouting(t *testing.T) {
	for _, path := range []string{"/events/hall-b", "/ingest/hall-b", "/export/hall-b/vtt", "/api/rooms/hall-b/talks", "/api/admin/rooms/hall-b/pause", "/api/admin/events/hall-b/context", "/r/hall-b", "/operator/hall-b"} {
		if id := roomID(path); id != "hall-b" {
			t.Errorf("%s routes to %q", path, id)
		}
	}
	for _, path := range []string{"/api/rooms", "/api/admin/status", "/metrics", "/healthz", "/api/admin/bank/entries"} {
		if id := roomID(path); id != "" {
			t.Errorf("%s unexpectedly routes to %q", path, id)
		}
	}
}
