// Copyright 2026 the Dubash contributors. Apache-2.0.

package asr

import (
	"strings"
	"unicode"
)

// Phrase boundaries for committing a long utterance while it is still
// being spoken. A caption line must read as the speaker's phrase: "भाई,
// तुम क्या कर रहे हो यार?" may not be cut after "भाई, तुम क्या". So a cut
// is allowed only where a phrase ends: after a clause-final verb, or
// before a word that opens a new clause, and never after a word that
// needs what follows (a pronoun, a question word, a postposition, an
// article).

// clauseFinal are words that end a Hindi clause (verbs and auxiliaries
// in the final position of an SOV sentence).
var clauseFinal = setOf(
	"है", "हैं", "था", "थे", "थी", "थीं", "हूँ", "हूं", "हो", "होगा", "होगी", "होंगे",
	"गया", "गए", "गई", "गयी", "दिया", "दिए", "दी", "लिया", "लिए", "ली", "किया", "किए", "की",
	"रहा", "रहे", "रही", "सकता", "सकते", "सकती", "चाहिए", "जाएगा", "जाएगी", "जाएँगे", "जाएंगे",
	"करेंगे", "करेगा", "करेगी", "करते", "करता", "करती", "जाता", "जाती", "जाते", "बोले", "बोला", "कहा",
)

// clauseOpen are words that start a new clause, in Hindi and English.
var clauseOpen = setOf(
	"और", "लेकिन", "पर", "मगर", "परंतु", "किंतु", "तो", "कि", "क्योंकि", "जब", "अगर", "यदि",
	"फिर", "इसलिए", "या", "बल्कि", "जो", "जिससे", "ताकि", "वरना",
	"and", "but", "so", "because", "when", "if", "then", "that", "which", "or", "while", "although", "since",
)

// trailing are words that close the phrase before them ("…कर रहे हो
// यार?"): a line may not end just before one.
var trailing = setOf("यार", "ना", "न", "जी", "भाई", "सर", "बाबा", "महाराज", "please", "sir", "na", "yaar")

// needsNext are words after which a phrase cannot end: pronouns, question
// words, postpositions, vocatives and articles that the next word
// completes.
var needsNext = setOf(
	"तुम", "आप", "हम", "मैं", "वो", "वह", "ये", "यह", "क्या", "क्यों", "कैसे", "कहाँ", "कहां", "कब", "कौन", "कितना",
	"का", "के", "की", "को", "से", "में", "पे", "ने", "भाई", "यार", "जी", "श्री", "डॉक्टर",
	"the", "a", "an", "to", "of", "in", "on", "at", "for", "with", "my", "your", "our", "their", "his", "her",
	"what", "why", "how", "where", "who", "very", "i", "we", "you", "he", "she", "they", "is", "are", "was", "will",
)

func setOf(ws ...string) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w] = true
	}
	return m
}

// bare is a word without the punctuation around it, in lower case.
func bare(w string) string {
	return strings.ToLower(strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) || isTerminal(r) }))
}

// phraseEnd reports whether a caption line may end after words[i] (with
// words[i+1] following).
func phraseEnd(words []string, i int) bool {
	if i < 0 || i+1 >= len(words) {
		return false
	}
	w, next := words[i], bare(words[i+1])
	last, _ := lastRune(w)
	switch {
	case isTerminal(last):
		return true // a sentence ended, whatever its last word
	case trailing[next] || needsNext[bare(w)]:
		return false
	case strings.ContainsRune(",;:", last):
		return true
	case clauseOpen[next]:
		return true
	case clauseFinal[bare(w)] && !clauseFinal[next]:
		// "जा रहे हैं" ends after "हैं", not after "रहे".
		return true
	}
	return false
}

func lastRune(s string) (rune, bool) {
	r := []rune(s)
	if len(r) == 0 {
		return 0, false
	}
	return r[len(r)-1], true
}

// lastPhraseEnd returns the largest n in [from, to] such that the first n
// words end a phrase, or 0 if none does.
func lastPhraseEnd(words []string, from, to int) int {
	for n := min(to, len(words)-1); n >= max(from, 1); n-- {
		if phraseEnd(words, n-1) {
			return n
		}
	}
	return 0
}
