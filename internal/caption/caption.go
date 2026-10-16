// Package caption holds the types shared across the captioning pipeline.
//
// Modified by the Dubash contributors: segments carry their stream epoch
// and revision, and translations travel as their own messages.
package caption

import "time"

// Segment is one finalized caption line: the original text plus its
// translations, timed against the room's audio clock (seconds).
type Segment struct {
	ID   int    `json:"id"`
	Room string `json:"room"`
	// Epoch numbers the room's transmissions (stream epochs): a reconnecting
	// operator starts a new one.
	Epoch int `json:"epoch,omitempty"`
	// Rev is the segment's revision: 0 as recognized, higher after a
	// correction.
	Rev int `json:"rev,omitempty"`
	// Talk numbers the talks given in the room, from 1, and TalkTitle
	// names the talk when the production team gave it a title.
	Talk      int       `json:"talk,omitempty"`
	TalkTitle string    `json:"talkTitle,omitempty"`
	At        time.Time `json:"at,omitzero"` // wall-clock time the line was transcribed
	T0        float64   `json:"t0"`
	T1        float64   `json:"t1"`
	Lang      string    `json:"lang"`
	Text      string    `json:"text"`
	// Raw is the recognized text when Text was corrected.
	Raw string `json:"raw,omitempty"`
	// Translations maps a language code to the text in that language.
	Translations map[string]string `json:"translations,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// In returns the segment text in lang, falling back to the original text
// when that translation is missing.
func (s Segment) In(lang string) string {
	if lang == s.Lang {
		return s.Text
	}
	if t := s.Translations[lang]; t != "" {
		return t
	}
	return s.Text
}

// Translation is one segment in one target language, sent to the readers
// of that language as soon as it is ready.
type Translation struct {
	Room    string `json:"room"`
	Segment int    `json:"segment"`
	Epoch   int    `json:"epoch,omitempty"`
	// Rev is the revision of the source segment this translates.
	Rev   int    `json:"rev,omitempty"`
	Lang  string `json:"lang"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

// Interim is the provisional text of the speech still in progress, in the
// spoken language. It is replaced by the next interim and never stored.
type Interim struct {
	Epoch int    `json:"epoch,omitempty"`
	Lang  string `json:"lang,omitempty"`
	Text  string `json:"text"`
}
