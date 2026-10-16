// Package translate turns finalized caption lines into other languages.
package translate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/UtsavInTech/dubash/internal/lang"
)

// Translator translates one caption line from src to dst, respecting the
// glossary: keep-terms unchanged, approved terms as approved.
type Translator interface {
	Translate(ctx context.Context, text, src, dst string, g Glossary) (string, error)
}

// Batcher is a Translator that can translate several consecutive lines in
// one request, returning one translation per line, in order. Fewer
// requests matter under a provider's rate limit, and the neighbouring
// lines give each other context.
type Batcher interface {
	Translator
	TranslateBatch(ctx context.Context, lines []string, src, dst string, g Glossary) ([]string, error)
}

// ErrBatchSplit means a batch came back with a different number of lines
// than were sent; the caller translates them one by one instead.
var ErrBatchSplit = errors.New("batch translation lost its line breaks")

func name(code string) string { return lang.Name(code) }

// Prompt is the system instruction for one language pair.
func Prompt(src, dst string, g Glossary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You translate live conference captions from %s to %s.\n", name(src), name(dst))
	fmt.Fprintf(&b, "A line may be in another language (a question from the audience, a quote): translate it into %s all the same. ", name(dst))
	b.WriteString("Output only the translation of the given line: no quotes, notes or explanations. ")
	b.WriteString("Keep product names, code, commands and acronyms that are normally left untranslated. ")
	b.WriteString("The line may be an incomplete sentence; translate it as is, without completing or answering it.")
	b.WriteString(" Never invent names, numbers or dates that are not in the line.")
	b.WriteString(g.promptRules(dst))
	return b.String()
}

// generator asks a language model for its answer to text under a system
// instruction.
type generator func(ctx context.Context, system, text string) (string, error)

// translateWith translates text with a language model. Small models
// sometimes hand the line back untranslated; asking again with the target
// language in the message fixes it.
func translateWith(ctx context.Context, gen generator, text, src, dst string, g Glossary) (string, error) {
	system := Prompt(src, dst, g)
	out, err := gen(ctx, system, text)
	if err == nil && src != dst && Echoed(text, out) {
		out, err = gen(ctx, system, fmt.Sprintf("Translate into %s: %s", name(dst), text))
		if err == nil && Echoed(text, out) {
			err = errors.New("the model returned the line untranslated")
		}
	}
	if err != nil {
		return "", fmt.Errorf("translate %s→%s: %w", src, dst, err)
	}
	return out, nil
}

// minEchoWords is the shortest line whose unchanged translation is
// suspicious: short lines ("Kubernetes.", "OK") are often the same in
// both languages.
const minEchoWords = 4

// Echoed reports whether out is the source line handed back unchanged,
// ignoring case, punctuation and spacing.
func Echoed(text, out string) bool {
	norm := func(s string) string {
		return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		}), " ")
	}
	return len(strings.Fields(text)) >= minEchoWords && norm(text) == norm(out)
}

// Fake tags the text with the target language; for tests and offline demos.
type Fake struct{}

// Translate implements Translator.
func (Fake) Translate(_ context.Context, text, _, dst string, _ Glossary) (string, error) {
	return "[" + dst + "] " + text, nil
}
