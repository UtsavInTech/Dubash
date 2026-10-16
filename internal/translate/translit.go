// Copyright 2026 the Dubash contributors. Apache-2.0.

package translate

import (
	"strings"
	"unicode"
)

// brahmic are the Unicode blocks of the Indian scripts that share
// Devanagari's layout (the ISCII order): the same sound sits at the same
// offset in each block.
var brahmic = []rune{0x0900, 0x0980, 0x0A00, 0x0A80, 0x0B00, 0x0B80, 0x0C00, 0x0C80, 0x0D00}

// toDevanagari maps a letter of any of those scripts onto Devanagari.
func toDevanagari(r rune) rune {
	for _, b := range brahmic {
		if r >= b && r < b+0x80 {
			return r - b + 0x0900
		}
	}
	return r
}

// soundKey is s reduced to its sounds: Brahmic letters mapped onto
// Devanagari, case folded, everything but letters and marks dropped.
func soundKey(s string) []rune {
	var out []rune
	for _, r := range strings.ToLower(s) {
		r = toDevanagari(r)
		if unicode.IsLetter(r) || unicode.IsMark(r) {
			out = append(out, r)
		}
	}
	return out
}

// minTranslitWords is the shortest line checked: short lines are often
// names, which are rightly written out in the target script.
const minTranslitWords = 4

// Transliterated reports whether out is src written in another Indian
// script rather than translated: the two sound alike for most of their
// length. Lines of only a few words are not judged.
func Transliterated(src, out string) bool {
	if len(strings.Fields(src)) < minTranslitWords {
		return false
	}
	a, b := soundKey(src), soundKey(out)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	d := editDistance(a, b)
	return 1-float64(d)/float64(max(len(a), len(b))) >= 0.6
}

func editDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// Garbled reports an answer that is no translation into an Indian script:
// src written out in dst's script (see Transliterated), or Latin letters
// mixed into words when src had none (a half-transliterated "రukiyో").
func Garbled(src, out, dst string) bool {
	if Transliterated(src, out) {
		return true
	}
	if dst == "en" || hasLatin(src) {
		return false
	}
	for _, w := range strings.Fields(out) {
		latin, other := false, false
		for _, r := range w {
			switch {
			case r < 0x80 && unicode.IsLetter(r):
				latin = true
			case unicode.IsLetter(r) || unicode.IsMark(r):
				other = true
			}
		}
		if latin && other {
			return true
		}
	}
	return false
}

func hasLatin(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x80 && unicode.IsLetter(r) }) >= 0
}
