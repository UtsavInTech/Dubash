package translate

import (
	"context"
	"strings"
	"testing"
)

func TestPromptNamesLanguagesAndGlossary(t *testing.T) {
	p := Prompt("en", "es", Keep("Kubernetes", "Nerdearla"))
	for _, want := range []string{"from English to Spanish", "Kubernetes, Nerdearla", "Output only the translation"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(Prompt("es", "en", Glossary{}), "Never translate these terms") {
		t.Error("empty glossary should not add the glossary clause")
	}
}

func TestFake(t *testing.T) {
	got, err := Fake{}.Translate(context.Background(), "Hello.", "en", "es", Glossary{})
	if err != nil || got != "[es] Hello." {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestEchoed(t *testing.T) {
	cases := []struct {
		text, out string
		want      bool
	}{
		{"Los subtítulos en tiempo real ayudan a personas sordas.", "Los subtítulos en tiempo real ayudan a personas sordas.", true},
		{"Los subtítulos en tiempo real ayudan a personas sordas.", "los subtítulos en tiempo real, ayudan a personas sordas", true},
		{"Los subtítulos en tiempo real ayudan a personas sordas.", "Real-time captions help deaf people.", false},
		{"Kubernetes y eBPF.", "Kubernetes y eBPF.", false}, // too short to tell
	}
	for _, c := range cases {
		if got := Echoed(c.text, c.out); got != c.want {
			t.Errorf("Echoed(%q, %q) = %v, want %v", c.text, c.out, got, c.want)
		}
	}
}
