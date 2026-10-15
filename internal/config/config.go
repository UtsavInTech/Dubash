// Package config loads the room definitions (rooms.yaml).
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"

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
