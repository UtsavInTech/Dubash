// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package bank is the Dictation Bank: the vocabulary an organisation, a
// speaker or an event teaches the captioning pipeline (names, brands,
// technical and religious terms, known mis-hearings, approved
// translations, words never to translate).
//
// Entries are versioned and only approved entries are used. A live event
// never reads the bank itself: it reads a context version, an immutable
// snapshot of the entries that apply to it (event over speaker over
// organisation), compiled and pinned by the admin. Editing the bank
// changes nothing live until a new version is pinned, and a pinned
// version takes effect at the next committed line.
package bank

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/UtsavInTech/dubash/internal/lang"
	"github.com/UtsavInTech/dubash/internal/script"
)

// Scope says whom an entry belongs to.
type Scope string

// Scopes, from the broadest to the most specific; a more specific entry
// wins over a broader one for the same term.
const (
	ScopeOrg     Scope = "org"
	ScopeSpeaker Scope = "speaker"
	ScopeEvent   Scope = "event"
)

func (s Scope) rank() int {
	switch s {
	case ScopeEvent:
		return 3
	case ScopeSpeaker:
		return 2
	case ScopeOrg:
		return 1
	}
	return 0
}

// Status is where an entry is in review.
type Status string

// Statuses. Only approved entries reach a context version.
const (
	Suggested Status = "suggested"
	Approved  Status = "approved"
	Rejected  Status = "rejected"
	Retired   Status = "retired"
)

// Sources of an entry.
const (
	SourceManual     = "manual"
	SourceCorrection = "correction" // suggested from a live word fix
	SourceScript     = "script"     // suggested from an uploaded script (phase 3)
)

// DefaultOrg is the organisation until accounts exist (phase 4).
const DefaultOrg = "default"

// Entry is one vocabulary entry, at one version.
type Entry struct {
	// ID is stable across versions; Version counts them from 1.
	ID      string `json:"id"`
	Version int    `json:"version"`
	Scope   Scope  `json:"scope"`
	// ScopeID names the organisation, the speaker (a slug of the name) or
	// the event (the room id).
	ScopeID string `json:"scopeId"`
	// Term is the canonical spelling, as captions should show it.
	Term string `json:"term"`
	// Lang is the language the term is said in; empty for any (a brand
	// name said inside Hindi and English alike).
	Lang string `json:"lang,omitempty"`
	// Aliases are other correct spellings; Misheard are wrong
	// recognitions seen for it (the correction stage maps them back).
	Aliases  []string `json:"aliases,omitempty"`
	Misheard []string `json:"misheard,omitempty"`
	// Pronunciation is a hint in plain text ("doo-baash").
	Pronunciation string `json:"pronunciation,omitempty"`
	// Meaning is what the term is: a person, a place, a ritual.
	Meaning string `json:"meaning,omitempty"`
	// Translations are approved renderings, by target language.
	Translations map[string]string `json:"translations,omitempty"`
	// Keep means never translate: shown as written in every language.
	Keep bool `json:"keep,omitempty"`
	// Ambiguous marks a term that is also an ordinary word: the
	// correction stage needs more evidence before using it.
	Ambiguous bool      `json:"ambiguous,omitempty"`
	Status    Status    `json:"status"`
	Source    string    `json:"source,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedBy string    `json:"createdBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Limits on what one entry may hold.
const (
	maxTermLen = 120
	maxList    = 50
)

// Validate checks e and normalizes its text (NFC, trimmed, no empty or
// duplicate list items).
func (e *Entry) Validate() error {
	e.Term = clean(e.Term)
	switch {
	case e.Term == "":
		return errors.New("bank: the term is empty")
	case utf8.RuneCountInString(e.Term) > maxTermLen:
		return fmt.Errorf("bank: the term is longer than %d characters", maxTermLen)
	case e.Scope.rank() == 0:
		return fmt.Errorf("bank: unknown scope %q", e.Scope)
	case e.ScopeID == "" && e.Scope != ScopeOrg:
		return fmt.Errorf("bank: a %s entry needs a scope id", e.Scope)
	case e.Lang != "" && !lang.Valid(e.Lang):
		return fmt.Errorf("bank: unknown language %q", e.Lang)
	}
	if e.Scope == ScopeOrg && e.ScopeID == "" {
		e.ScopeID = DefaultOrg
	}
	if e.Scope == ScopeSpeaker {
		e.ScopeID = Slug(e.ScopeID)
	}
	if e.Status == "" {
		e.Status = Suggested
	}
	if !slices.Contains([]Status{Suggested, Approved, Rejected, Retired}, e.Status) {
		return fmt.Errorf("bank: unknown status %q", e.Status)
	}
	var err error
	if e.Aliases, err = cleanList(e.Aliases, e.Term); err != nil {
		return err
	}
	if e.Misheard, err = cleanList(e.Misheard, e.Term); err != nil {
		return err
	}
	for l, t := range e.Translations {
		if !lang.Valid(l) {
			return fmt.Errorf("bank: translation into unknown language %q", l)
		}
		if t = clean(t); t == "" {
			delete(e.Translations, l)
		} else {
			e.Translations[l] = t
		}
	}
	e.Pronunciation, e.Meaning, e.Note = clean(e.Pronunciation), clean(e.Meaning), clean(e.Note)
	return nil
}

func clean(s string) string {
	return strings.Join(strings.Fields(norm.NFC.String(s)), " ")
}

func cleanList(items []string, term string) ([]string, error) {
	var out []string
	for _, it := range items {
		it = clean(it)
		if it == "" || Fold(it) == Fold(term) || slices.ContainsFunc(out, func(o string) bool { return Fold(o) == Fold(it) }) {
			continue
		}
		if utf8.RuneCountInString(it) > maxTermLen {
			return nil, fmt.Errorf("bank: %q is longer than %d characters", it, maxTermLen)
		}
		out = append(out, it)
	}
	if len(out) > maxList {
		return nil, fmt.Errorf("bank: more than %d items in a list", maxList)
	}
	return out, nil
}

// Fold is the key two spellings are compared by: NFC, lower case, single
// spaces, Devanagari nukta dropped.
func Fold(s string) string {
	s = strings.ReplaceAll(norm.NFD.String(strings.ToLower(s)), "़", "")
	return strings.Join(strings.Fields(norm.NFC.String(s)), " ")
}

// Slug turns a speaker's name into a scope id.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(norm.NFC.String(name)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// Spellings are the term and its aliases.
func (e Entry) Spellings() []string { return append([]string{e.Term}, e.Aliases...) }

// Resolve picks the approved entries that apply to an event with a
// speaker, applying precedence: for the same term (and language), the
// event's entry wins over the speaker's, which wins over the
// organisation's. The result is ordered event entries first, then
// speaker, then organisation, each by term.
func Resolve(entries []Entry, event, speaker string) []Entry {
	speaker = Slug(speaker)
	best := map[string]Entry{}
	for _, e := range entries {
		if e.Status != Approved {
			continue
		}
		switch {
		case e.Scope == ScopeEvent && e.ScopeID == event:
		case e.Scope == ScopeSpeaker && speaker != "" && e.ScopeID == speaker:
		case e.Scope == ScopeOrg:
		default:
			continue
		}
		key := e.Lang + "\x00" + Fold(e.Term)
		if cur, ok := best[key]; !ok || e.Scope.rank() > cur.Scope.rank() {
			best[key] = e
		}
	}
	out := make([]Entry, 0, len(best))
	for _, e := range best {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Entry) int {
		if a.Scope.rank() != b.Scope.rank() {
			return b.Scope.rank() - a.Scope.rank()
		}
		if c := strings.Compare(Fold(a.Term), Fold(b.Term)); c != 0 {
			return c
		}
		if c := strings.Compare(a.Lang, b.Lang); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// Version is an immutable snapshot of what applies to one event.
type Version struct {
	ID        int64     `json:"id"`
	Event     string    `json:"event"`
	Number    int       `json:"number"` // 1, 2… per event
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy,omitempty"`
	Note      string    `json:"note,omitempty"`
	Speaker   string    `json:"speaker,omitempty"`
	Entries   []Entry   `json:"entries"`
	// Script holds the chunks of the event's active scripts when the
	// version was compiled (see AttachScripts).
	Script []script.Chunk `json:"script,omitempty"`
	// Hash identifies the content, so pinning an identical snapshot twice
	// is visible.
	Hash string `json:"hash"`
}

// NewVersion snapshots the entries that apply to event.
func NewVersion(all []Entry, event, speaker, actor, note string) Version {
	entries := Resolve(all, event, speaker)
	data, _ := json.Marshal(entries)
	sum := sha256.Sum256(data)
	return Version{
		Event: event, Speaker: speaker, CreatedAt: time.Now().UTC(), CreatedBy: actor, Note: note,
		Entries: entries, Hash: hex.EncodeToString(sum[:8]),
	}
}

// Correction records a live word fix of a committed line.
type Correction struct {
	ID        int64     `json:"id"`
	Event     string    `json:"event"`
	Segment   int       `json:"segment"`
	Rev       int       `json:"rev"`
	Raw       string    `json:"raw"`
	Corrected string    `json:"corrected"`
	Lang      string    `json:"lang"`
	At        time.Time `json:"at"`
	By        string    `json:"by,omitempty"`
	// Suggested are the ids of the entries suggested from this fix.
	Suggested []string `json:"suggested,omitempty"`
}

// AuditEntry is one write to the bank, for the audit log.
type AuditEntry struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Object string    `json:"object"`
	Detail string    `json:"detail,omitempty"`
}
