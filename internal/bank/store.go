// Copyright 2026 the Dubash contributors. Apache-2.0.

package bank

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrNotFound is returned for an unknown entry or version.
var ErrNotFound = errors.New("bank: not found")

// Filter selects entries; empty fields select everything.
type Filter struct {
	Scope   Scope
	ScopeID string
	Status  Status
	// Query matches the term, aliases or misheard spellings (folded).
	Query string
}

func (f Filter) match(e Entry) bool {
	if f.Scope != "" && e.Scope != f.Scope || f.ScopeID != "" && e.ScopeID != f.ScopeID || f.Status != "" && e.Status != f.Status {
		return false
	}
	if f.Query == "" {
		return true
	}
	q := Fold(f.Query)
	return slices.ContainsFunc(append(e.Spellings(), e.Misheard...), func(s string) bool { return strings.Contains(Fold(s), q) })
}

// Store keeps the bank. Every write is versioned or appended, never
// overwritten, and audited. The live pipeline never calls a Store: it
// reads a Context compiled from a pinned Version.
type Store interface {
	// Entries returns the current version of each entry matching f,
	// ordered by scope then term.
	Entries(ctx context.Context, f Filter) ([]Entry, error)
	// Entry returns the current version of an entry.
	Entry(ctx context.Context, id string) (Entry, error)
	// History returns every version of an entry, oldest first.
	History(ctx context.Context, id string) ([]Entry, error)
	// SaveEntry validates e and stores it: a new entry when e.ID is
	// empty, else a new version of that entry.
	SaveEntry(ctx context.Context, e Entry, actor string) (Entry, error)

	// SaveVersion stores a context version, numbering it for its event.
	SaveVersion(ctx context.Context, v Version, actor string) (Version, error)
	Version(ctx context.Context, id int64) (Version, error)
	// Versions lists an event's versions, newest first, without entries.
	Versions(ctx context.Context, event string) ([]Version, error)
	// Pin makes a version the one an event uses.
	Pin(ctx context.Context, event string, id int64, actor string) error
	// Pinned returns the event's pinned version, if any.
	Pinned(ctx context.Context, event string) (Version, bool, error)

	SaveCorrection(ctx context.Context, c Correction) (Correction, error)
	Corrections(ctx context.Context, event string) ([]Correction, error)

	// Audit returns the latest audit entries, newest first.
	Audit(ctx context.Context, limit int) ([]AuditEntry, error)
	Close() error
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sortEntries(es []Entry) {
	slices.SortFunc(es, func(a, b Entry) int {
		if a.Scope.rank() != b.Scope.rank() {
			return b.Scope.rank() - a.Scope.rank()
		}
		if a.ScopeID != b.ScopeID {
			return strings.Compare(a.ScopeID, b.ScopeID)
		}
		return strings.Compare(Fold(a.Term), Fold(b.Term))
	})
}

// FileStore keeps the bank in one JSON file, rewritten atomically on each
// write: for a single machine without PostgreSQL, and for tests.
type FileStore struct {
	path string
	mu   sync.Mutex
	data fileData
}

type fileData struct {
	Entries     []Entry          `json:"entries"` // every version
	Versions    []Version        `json:"versions"`
	Pins        map[string]int64 `json:"pins"`
	Corrections []Correction     `json:"corrections"`
	Audit       []AuditEntry     `json:"audit"`
}

// OpenFile opens (or creates) the bank file at path.
func OpenFile(path string) (*FileStore, error) {
	s := &FileStore{path: path, data: fileData{Pins: map[string]int64{}}}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("bank: %w", err)
	default:
		if err := json.Unmarshal(data, &s.data); err != nil {
			return nil, fmt.Errorf("bank: %s: %w", path, err)
		}
		if s.data.Pins == nil {
			s.data.Pins = map[string]int64{}
		}
	}
	return s, nil
}

// save writes the file atomically. The caller holds mu.
func (s *FileStore) save() error {
	data, err := json.MarshalIndent(s.data, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *FileStore) audit(actor, action, object, detail string) {
	s.data.Audit = append(s.data.Audit, AuditEntry{At: time.Now().UTC(), Actor: actor, Action: action, Object: object, Detail: detail})
}

// current returns the latest version of every entry. The caller holds mu.
func (s *FileStore) current() []Entry {
	latest := map[string]Entry{}
	for _, e := range s.data.Entries {
		if cur, ok := latest[e.ID]; !ok || e.Version > cur.Version {
			latest[e.ID] = e
		}
	}
	out := make([]Entry, 0, len(latest))
	for _, e := range latest {
		out = append(out, e)
	}
	return out
}

// Entries implements Store.
func (s *FileStore) Entries(_ context.Context, f Filter) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	for _, e := range s.current() {
		if f.match(e) {
			out = append(out, e)
		}
	}
	sortEntries(out)
	return out, nil
}

// Entry implements Store.
func (s *FileStore) Entry(_ context.Context, id string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.current() {
		if e.ID == id {
			return e, nil
		}
	}
	return Entry{}, ErrNotFound
}

// History implements Store.
func (s *FileStore) History(_ context.Context, id string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
	for _, e := range s.data.Entries {
		if e.ID == id {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	slices.SortFunc(out, func(a, b Entry) int { return a.Version - b.Version })
	return out, nil
}

// SaveEntry implements Store.
func (s *FileStore) SaveEntry(_ context.Context, e Entry, actor string) (Entry, error) {
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	action := "create"
	if e.ID == "" {
		e.ID, e.Version = newID(), 1
	} else {
		v := 0
		for _, old := range s.data.Entries {
			if old.ID == e.ID {
				v = max(v, old.Version)
			}
		}
		if v == 0 {
			return Entry{}, ErrNotFound
		}
		e.Version = v + 1
		action = "update"
	}
	e.CreatedBy, e.UpdatedAt = actor, time.Now().UTC()
	s.data.Entries = append(s.data.Entries, e)
	s.audit(actor, action, "entry:"+e.ID, fmt.Sprintf("v%d %s %q", e.Version, e.Status, e.Term))
	return e, s.save()
}

// SaveVersion implements Store.
func (s *FileStore) SaveVersion(_ context.Context, v Version, actor string) (Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var maxID int64
	n := 0
	for _, old := range s.data.Versions {
		maxID = max(maxID, old.ID)
		if old.Event == v.Event {
			n = max(n, old.Number)
		}
	}
	v.ID, v.Number, v.CreatedBy = maxID+1, n+1, actor
	s.data.Versions = append(s.data.Versions, v)
	s.audit(actor, "compile", fmt.Sprintf("event:%s", v.Event), fmt.Sprintf("version %d, %d entries, %s", v.Number, len(v.Entries), v.Hash))
	return v, s.save()
}

// Version implements Store.
func (s *FileStore) Version(_ context.Context, id int64) (Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.data.Versions {
		if v.ID == id {
			return v, nil
		}
	}
	return Version{}, ErrNotFound
}

// Versions implements Store.
func (s *FileStore) Versions(_ context.Context, event string) ([]Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Version
	for _, v := range s.data.Versions {
		if v.Event == event {
			v.Entries = nil
			out = append(out, v)
		}
	}
	slices.SortFunc(out, func(a, b Version) int { return b.Number - a.Number })
	return out, nil
}

// Pin implements Store.
func (s *FileStore) Pin(_ context.Context, event string, id int64, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.ContainsFunc(s.data.Versions, func(v Version) bool { return v.ID == id && v.Event == event }) {
		return ErrNotFound
	}
	s.data.Pins[event] = id
	s.audit(actor, "pin", "event:"+event, fmt.Sprintf("version id %d", id))
	return s.save()
}

// Pinned implements Store.
func (s *FileStore) Pinned(_ context.Context, event string) (Version, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.data.Pins[event]
	if !ok {
		return Version{}, false, nil
	}
	for _, v := range s.data.Versions {
		if v.ID == id {
			return v, true, nil
		}
	}
	return Version{}, false, nil
}

// SaveCorrection implements Store.
func (s *FileStore) SaveCorrection(_ context.Context, c Correction) (Correction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.ID = int64(len(s.data.Corrections) + 1)
	s.data.Corrections = append(s.data.Corrections, c)
	s.audit(c.By, "correct", fmt.Sprintf("event:%s segment:%d", c.Event, c.Segment), fmt.Sprintf("%q → %q", c.Raw, c.Corrected))
	return c, s.save()
}

// Corrections implements Store.
func (s *FileStore) Corrections(_ context.Context, event string) ([]Correction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Correction
	for _, c := range s.data.Corrections {
		if event == "" || c.Event == event {
			out = append(out, c)
		}
	}
	return out, nil
}

// Audit implements Store.
func (s *FileStore) Audit(_ context.Context, limit int) ([]AuditEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone(s.data.Audit)
	slices.Reverse(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Close implements Store.
func (s *FileStore) Close() error { return nil }
