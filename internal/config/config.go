// Package config loads the room definitions (rooms.yaml).
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/UtsavInTech/dubash/internal/lang"
)

// Room is one stage/session that receives audio and produces captions.
type Room struct {
	ID       string   `yaml:"id" json:"id"`
	Title    string   `yaml:"title" json:"title"`
	Source   string   `yaml:"source" json:"source"`
	Targets  []string `yaml:"targets" json:"targets"`
	Glossary []string `yaml:"glossary" json:"glossary,omitempty"`
	// Speaker names who speaks, for the audience's welcome line.
	Speaker string `yaml:"speaker" json:"speaker,omitempty"`
	// ASRMode is the recognizer's style; for Sarvam "codemix" writes
	// English words in Latin script inside Hindi captions (Hinglish), the
	// default writes them in the language's own script.
	ASRMode string `yaml:"asr_mode" json:"-"`
	// Prompt is a short description of the event, a hint for recognizers
	// that take one.
	Prompt string `yaml:"prompt" json:"-"`
	// AlwaysTranslate lists target languages translated even while nobody
	// reads them (for the transcript export). Others are translated only
	// while someone reads them.
	AlwaysTranslate []string `yaml:"always_translate" json:"-"`

	// Access and privacy (phase 5).
	//
	// AudiencePIN, when set, is asked of readers before they see the
	// captions, history or transcript (4 to 12 digits).
	AudiencePIN string `yaml:"audience_pin" json:"-"`
	// Exports says who may download the transcript: readers (anyone who
	// can read the captions; the default), admin, or off.
	Exports string `yaml:"exports" json:"-"`
	// JoinRevision is part of the room's QR join code (/j/{code}): change
	// it to make printed codes stop working.
	JoinRevision int `yaml:"join_revision" json:"-"`
	// StoreTranscript, when false, keeps nothing on disk: lines are only
	// relayed live (and replayed from memory to late readers).
	StoreTranscript *bool `yaml:"store_transcript" json:"-"`
	// RetentionDays deletes stored lines older than this many days; 0
	// keeps them until deleted from the admin panel.
	RetentionDays int `yaml:"retention_days" json:"-"`
}

// Stores reports whether the room keeps its transcript on disk.
func (r Room) Stores() bool { return r.StoreTranscript == nil || *r.StoreTranscript }

// Export policies.
const (
	ExportReaders = "readers"
	ExportAdmin   = "admin"
	ExportOff     = "off"
)

// ExportPolicy is Exports with its default.
func (r Room) ExportPolicy() string {
	if r.Exports == "" {
		return ExportReaders
	}
	return r.Exports
}

// Languages are every language the room serves, spoken or translated:
// Source first, then Targets. Any of them can be the one spoken in a given
// transmission; the others are then its translations.
func (r Room) Languages() []string {
	return append([]string{r.Source}, r.Targets...)
}

// Config is the whole rooms.yaml file.
type Config struct {
	Rooms []Room `yaml:"rooms"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// maxGlossary is the most terms the Live API takes as custom vocabulary.
const maxGlossary = 1000

// Load reads and validates a rooms file.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read rooms: %w", err)
	}
	return Parse(data)
}

// Parse validates rooms YAML and fills defaults (title defaults to id,
// targets to an empty list).
func Parse(data []byte) (Config, error) {
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse rooms: %w", err)
	}
	if len(c.Rooms) == 0 {
		return Config{}, errors.New("rooms: at least one room is required")
	}
	seen := map[string]bool{}
	for i, r := range c.Rooms {
		if !idPattern.MatchString(r.ID) {
			return Config{}, fmt.Errorf("room %d: invalid id %q (use lowercase letters, digits and dashes)", i, r.ID)
		}
		if seen[r.ID] {
			return Config{}, fmt.Errorf("room %q: duplicate id", r.ID)
		}
		seen[r.ID] = true
		if !lang.Valid(r.Source) {
			return Config{}, fmt.Errorf("room %q: unsupported source language %q", r.ID, r.Source)
		}
		for j, t := range r.Targets {
			if !lang.Valid(t) || t == r.Source || slices.Contains(r.Targets[:j], t) {
				return Config{}, fmt.Errorf("room %q: invalid target language %q", r.ID, t)
			}
		}
		for _, l := range r.AlwaysTranslate {
			if !slices.Contains(r.Languages(), l) {
				return Config{}, fmt.Errorf("room %q: always_translate %q is not one of its languages", r.ID, l)
			}
		}
		if p := r.AudiencePIN; p != "" && (len(p) < 4 || len(p) > 12 || strings.Trim(p, "0123456789") != "") {
			return Config{}, fmt.Errorf("room %q: audience_pin must be 4 to 12 digits", r.ID)
		}
		if !slices.Contains([]string{"", ExportReaders, ExportAdmin, ExportOff}, r.Exports) {
			return Config{}, fmt.Errorf("room %q: exports must be readers, admin or off", r.ID)
		}
		if r.RetentionDays < 0 {
			return Config{}, fmt.Errorf("room %q: retention_days must not be negative", r.ID)
		}
		if len(r.Glossary) > maxGlossary {
			return Config{}, fmt.Errorf("room %q: glossary has %d terms, at most %d are allowed", r.ID, len(r.Glossary), maxGlossary)
		}
		if r.Title == "" {
			c.Rooms[i].Title = r.ID
		}
		if r.Targets == nil {
			c.Rooms[i].Targets = []string{} // served as JSON: [] not null
		}
	}
	return c, nil
}
