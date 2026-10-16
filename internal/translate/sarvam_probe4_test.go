// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// TestProbeUIStrings has Sarvam translate the viewer's English interface
// independently, to compare with ours (UI_STRINGS: the JSON of the
// tables). By hand:
//
//	SARVAM_PROBE=1 UI_STRINGS=ui.json go test -run ProbeUIStrings -v ./internal/translate
func TestProbeUIStrings(t *testing.T) {
	key, path := os.Getenv("SARVAM_API_KEY"), os.Getenv("UI_STRINGS")
	if key == "" || os.Getenv("SARVAM_PROBE") == "" || path == "" {
		t.Skip("set SARVAM_API_KEY, SARVAM_PROBE=1 and UI_STRINGS")
	}
	data, _ := os.ReadFile(path)
	var ui map[string]map[string]string
	if err := json.Unmarshal(data, &ui); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range ui["en"] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var lines []string
	for _, k := range keys {
		lines = append(lines, ui["en"][k])
	}
	s := SarvamChat{Key: key}
	g := Glossary{Keep: []string{"{title}", "{name}", "TXT", "SRT", "VTT"}}
	for _, dst := range []string{"hi", "te", "kn", "ta"} {
		var got []string
		for i := 0; i < len(lines); i += 12 {
			part, err := s.TranslateBatch(context.Background(), lines[i:min(i+12, len(lines))], "en", dst, g)
			if err != nil {
				t.Fatalf("%s: %v", dst, err)
			}
			got = append(got, part...)
		}
		for i, k := range keys {
			t.Logf("%s\t%s\tOURS: %s\tSARVAM: %s", dst, k, ui[dst][k], got[i])
		}
	}
}
