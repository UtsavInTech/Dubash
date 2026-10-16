// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package lang is the one list of languages Dubash serves, with the codes
// each provider expects. Every other package asks it instead of keeping
// its own list; internal/web/static/js/langs.js mirrors it for the pages
// (a test keeps the two in step).
package lang

import "slices"

// Language is one language a room may speak or translate into.
type Language struct {
	// Code is the ISO 639-1 code used across Dubash: URLs, rooms.yaml,
	// stored segments.
	Code string
	// Name is the English name, used in prompts and logs.
	Name string
	// Native is the name in the language itself, as a reader looks for it.
	Native string
	// Script is the ISO 15924 code of the script it is normally written in.
	Script string
	// Indian is true for the languages of India (English is listed with
	// them as en-IN for providers, but is not Indian here).
	Indian bool
	// Sarvam is the tag Sarvam's speech-to-text and translate APIs take,
	// empty when Sarvam does not support the language.
	Sarvam string
	// SarvamRealtime is the tag for Sarvam's realtime WebSocket, which
	// differs from Sarvam for Odia (or-IN there, od-IN elsewhere).
	SarvamRealtime string
	// BCP47 is the tag for other providers (Gemini Live, browsers).
	BCP47 string
}

// all languages, in the order pages list them: English, then the
// languages of India by number of speakers, then the European languages
// upstream Lenguaraz served.
var all = []Language{
	{Code: "en", Name: "English", Native: "English", Script: "Latn", Sarvam: "en-IN", SarvamRealtime: "en-IN", BCP47: "en-IN"},
	{Code: "hi", Name: "Hindi", Native: "हिन्दी", Script: "Deva", Indian: true, Sarvam: "hi-IN", SarvamRealtime: "hi-IN", BCP47: "hi-IN"},
	{Code: "bn", Name: "Bengali", Native: "বাংলা", Script: "Beng", Indian: true, Sarvam: "bn-IN", SarvamRealtime: "bn-IN", BCP47: "bn-IN"},
	{Code: "mr", Name: "Marathi", Native: "मराठी", Script: "Deva", Indian: true, Sarvam: "mr-IN", SarvamRealtime: "mr-IN", BCP47: "mr-IN"},
	{Code: "te", Name: "Telugu", Native: "తెలుగు", Script: "Telu", Indian: true, Sarvam: "te-IN", SarvamRealtime: "te-IN", BCP47: "te-IN"},
	{Code: "ta", Name: "Tamil", Native: "தமிழ்", Script: "Taml", Indian: true, Sarvam: "ta-IN", SarvamRealtime: "ta-IN", BCP47: "ta-IN"},
	{Code: "gu", Name: "Gujarati", Native: "ગુજરાતી", Script: "Gujr", Indian: true, Sarvam: "gu-IN", SarvamRealtime: "gu-IN", BCP47: "gu-IN"},
	{Code: "kn", Name: "Kannada", Native: "ಕನ್ನಡ", Script: "Knda", Indian: true, Sarvam: "kn-IN", SarvamRealtime: "kn-IN", BCP47: "kn-IN"},
	{Code: "ml", Name: "Malayalam", Native: "മലയാളം", Script: "Mlym", Indian: true, Sarvam: "ml-IN", SarvamRealtime: "ml-IN", BCP47: "ml-IN"},
	{Code: "or", Name: "Odia", Native: "ଓଡ଼ିଆ", Script: "Orya", Indian: true, Sarvam: "od-IN", SarvamRealtime: "or-IN", BCP47: "or-IN"},
	{Code: "pa", Name: "Punjabi", Native: "ਪੰਜਾਬੀ", Script: "Guru", Indian: true, Sarvam: "pa-IN", SarvamRealtime: "pa-IN", BCP47: "pa-IN"},
	{Code: "es", Name: "Spanish", Native: "Español", Script: "Latn", BCP47: "es-ES"},
	{Code: "pt", Name: "Portuguese", Native: "Português", Script: "Latn", BCP47: "pt-BR"},
	{Code: "fr", Name: "French", Native: "Français", Script: "Latn", BCP47: "fr-FR"},
	{Code: "de", Name: "German", Native: "Deutsch", Script: "Latn", BCP47: "de-DE"},
	{Code: "it", Name: "Italian", Native: "Italiano", Script: "Latn", BCP47: "it-IT"},
}

var byCode = func() map[string]Language {
	m := make(map[string]Language, len(all))
	for _, l := range all {
		m[l.Code] = l
	}
	return m
}()

// All returns every language, in display order.
func All() []Language { return slices.Clone(all) }

// Codes returns every language code, in display order.
func Codes() []string {
	out := make([]string, len(all))
	for i, l := range all {
		out[i] = l.Code
	}
	return out
}

// Get returns the language with code.
func Get(code string) (Language, bool) {
	l, ok := byCode[code]
	return l, ok
}

// Valid reports whether code is a language Dubash serves.
func Valid(code string) bool {
	_, ok := byCode[code]
	return ok
}

// Name returns the English name of code, or code itself when unknown.
func Name(code string) string {
	if l, ok := byCode[code]; ok {
		return l.Name
	}
	return code
}

// Sarvam returns the Sarvam REST/translate tag for code, or "" when
// Sarvam does not support it.
func Sarvam(code string) string { return byCode[code].Sarvam }

// SarvamRealtime returns the Sarvam realtime WebSocket tag for code, or ""
// when Sarvam does not support it.
func SarvamRealtime(code string) string { return byCode[code].SarvamRealtime }

// BCP47 returns the general BCP 47 tag for code, or "" when unknown.
func BCP47(code string) string { return byCode[code].BCP47 }
