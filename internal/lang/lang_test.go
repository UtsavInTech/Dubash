// Copyright 2026 the Dubash contributors. Apache-2.0.

package lang

import (
	"os"
	"regexp"
	"slices"
	"testing"
)

func TestRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range All() {
		if seen[l.Code] || l.Name == "" || l.Native == "" || l.Script == "" || l.BCP47 == "" {
			t.Errorf("bad or duplicate entry %+v", l)
		}
		seen[l.Code] = true
		if l.Indian && (l.Sarvam == "" || l.SarvamRealtime == "") {
			t.Errorf("%s: Indian language without Sarvam codes", l.Code)
		}
	}
	if Sarvam("or") != "od-IN" || SarvamRealtime("or") != "or-IN" {
		t.Error("Odia codes differ between Sarvam APIs")
	}
	if Name("xx") != "xx" || Valid("xx") || !Valid("kn") {
		t.Error("lookup of unknown codes")
	}
}

// TestPagesListTheSameLanguages keeps langs.js, which the pages import, in
// step with the registry.
func TestPagesListTheSameLanguages(t *testing.T) {
	data, err := os.ReadFile("../web/static/js/langs.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`export const NAMES = \{([^}]*)\}`).FindSubmatch(data)
	if m == nil {
		t.Fatal("NAMES not found in langs.js")
	}
	var js []string
	for _, kv := range regexp.MustCompile(`(\w+): '([^']*)'`).FindAllSubmatch(m[1], -1) {
		code, native := string(kv[1]), string(kv[2])
		js = append(js, code)
		if l, ok := Get(code); !ok || l.Native != native {
			t.Errorf("langs.js %s: %q, registry has %+v", code, native, l)
		}
	}
	if !slices.Equal(js, Codes()) {
		t.Errorf("langs.js lists %v, registry %v", js, Codes())
	}
}
