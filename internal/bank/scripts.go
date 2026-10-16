// Copyright 2026 the Dubash contributors. Apache-2.0.

package bank

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/UtsavInTech/dubash/internal/script"
)

// Script statuses: an active script goes into the event's next version.
const (
	ScriptActive  = "active"
	ScriptRetired = "retired"
)

// maxScriptChars bounds one script's text.
const maxScriptChars = 2_000_000

// Script is a document the speaker plans to follow, at one version.
type Script struct {
	ID         string    `json:"id"`
	Version    int       `json:"version"`
	Event      string    `json:"event"`
	Name       string    `json:"name"`
	Text       string    `json:"text,omitempty"`
	Status     string    `json:"status"`
	UploadedAt time.Time `json:"uploadedAt"`
	UploadedBy string    `json:"uploadedBy,omitempty"`
}

func (s *Script) validate() error {
	s.Name = clean(s.Name)
	s.Text = script.Clean(s.Text)
	switch {
	case s.Event == "":
		return errors.New("bank: a script needs an event")
	case s.Name == "":
		return errors.New("bank: a script needs a name")
	case s.Text == "":
		return errors.New("bank: the script is empty")
	case utf8.RuneCountInString(s.Text) > maxScriptChars:
		return fmt.Errorf("bank: the script is longer than %d characters", maxScriptChars)
	}
	if s.Status == "" {
		s.Status = ScriptActive
	}
	if s.Status != ScriptActive && s.Status != ScriptRetired {
		return fmt.Errorf("bank: unknown script status %q", s.Status)
	}
	return nil
}

// AttachScripts adds the chunks of the active scripts (in upload order)
// to a version being compiled, and updates its hash.
func (v *Version) AttachScripts(scripts []Script) {
	v.Script = nil
	for _, s := range scripts {
		if s.Status == ScriptActive && s.Event == v.Event {
			v.Script = append(v.Script, script.Split(s.Name, s.Text, len(v.Script))...)
		}
	}
	data, _ := json.Marshal(struct {
		E []Entry
		S []script.Chunk
	}{v.Entries, v.Script})
	sum := sha256.Sum256(data)
	v.Hash = hex.EncodeToString(sum[:8])
}

// ---------- FileStore ----------

// SaveScript implements Store.
func (s *FileStore) SaveScript(_ context.Context, sc Script, actor string) (Script, error) {
	if err := sc.validate(); err != nil {
		return Script{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	action := "upload"
	if sc.ID == "" {
		sc.ID, sc.Version = newID(), 1
	} else {
		v := 0
		for _, old := range s.data.Scripts {
			if old.ID == sc.ID {
				v = max(v, old.Version)
			}
		}
		if v == 0 {
			return Script{}, ErrNotFound
		}
		sc.Version, action = v+1, "update"
	}
	sc.UploadedAt, sc.UploadedBy = time.Now().UTC(), actor
	s.data.Scripts = append(s.data.Scripts, sc)
	s.audit(actor, action+" script", "event:"+sc.Event, fmt.Sprintf("%q v%d %s, %d characters", sc.Name, sc.Version, sc.Status, utf8.RuneCountInString(sc.Text)))
	return sc, s.save()
}

// Scripts implements Store.
func (s *FileStore) Scripts(_ context.Context, event string) ([]Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	latest := map[string]Script{}
	var order []string
	for _, sc := range s.data.Scripts {
		if sc.Event != event {
			continue
		}
		if _, ok := latest[sc.ID]; !ok {
			order = append(order, sc.ID)
		}
		if cur, ok := latest[sc.ID]; !ok || sc.Version > cur.Version {
			latest[sc.ID] = sc
		}
	}
	var out []Script
	for _, id := range order {
		out = append(out, latest[id])
	}
	return out, nil
}

// ---------- PGStore ----------

// SaveScript implements Store.
func (s *PGStore) SaveScript(ctx context.Context, sc Script, actor string) (Script, error) {
	if err := sc.validate(); err != nil {
		return Script{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Script{}, fmt.Errorf("bank: %w", err)
	}
	defer tx.Rollback(ctx)
	action := "upload"
	if sc.ID == "" {
		sc.ID, sc.Version = newID(), 1
	} else {
		var v int
		err := tx.QueryRow(ctx, `SELECT version FROM script_documents WHERE script_id = $1 AND current FOR UPDATE`, sc.ID).Scan(&v)
		if errors.Is(err, pgx.ErrNoRows) {
			return Script{}, ErrNotFound
		}
		if err != nil {
			return Script{}, fmt.Errorf("bank: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE script_documents SET current = false WHERE script_id = $1 AND current`, sc.ID); err != nil {
			return Script{}, fmt.Errorf("bank: %w", err)
		}
		sc.Version, action = v+1, "update"
	}
	sc.UploadedAt, sc.UploadedBy = time.Now().UTC().Truncate(time.Microsecond), actor
	if _, err := tx.Exec(ctx, `INSERT INTO script_documents (script_id, version, event_id, name, text, status, uploaded_at, uploaded_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, sc.ID, sc.Version, sc.Event, sc.Name, sc.Text, sc.Status, sc.UploadedAt, sc.UploadedBy); err != nil {
		return Script{}, fmt.Errorf("bank: %w", err)
	}
	if err := s.audit(ctx, tx, actor, action+" script", "event:"+sc.Event, fmt.Sprintf("%q v%d %s, %d characters", sc.Name, sc.Version, sc.Status, utf8.RuneCountInString(sc.Text))); err != nil {
		return Script{}, err
	}
	return sc, tx.Commit(ctx)
}

// Scripts implements Store.
func (s *PGStore) Scripts(ctx context.Context, event string) ([]Script, error) {
	rows, err := s.db.Query(ctx, `SELECT script_id, version, event_id, name, text, status, uploaded_at, uploaded_by
		FROM script_documents WHERE current AND event_id = $1
		ORDER BY (SELECT MIN(row_id) FROM script_documents d WHERE d.script_id = script_documents.script_id)`, event)
	if err != nil {
		return nil, fmt.Errorf("bank: %w", err)
	}
	defer rows.Close()
	var out []Script
	for rows.Next() {
		var sc Script
		if err := rows.Scan(&sc.ID, &sc.Version, &sc.Event, &sc.Name, &sc.Text, &sc.Status, &sc.UploadedAt, &sc.UploadedBy); err != nil {
			return nil, err
		}
		sc.UploadedAt = sc.UploadedAt.UTC()
		out = append(out, sc)
	}
	return out, rows.Err()
}

// knownTerm reports whether a term is already in entries (any spelling).
func knownTerm(entries []Entry, term string) bool {
	k := Fold(term)
	return slices.ContainsFunc(entries, func(e Entry) bool {
		return e.Status != Rejected && slices.ContainsFunc(e.Spellings(), func(s string) bool { return Fold(s) == k })
	})
}

// ScriptSuggestions lists the candidate terms of a script that the
// event's bank (event, speaker and organisation entries) does not have.
func ScriptSuggestions(ctx context.Context, s Store, sc Script) ([]script.Candidate, error) {
	all, err := s.Entries(ctx, Filter{})
	if err != nil {
		return nil, err
	}
	var out []script.Candidate
	for _, c := range script.SuggestTerms(sc.Text) {
		if !knownTerm(all, c.Term) && !strings.EqualFold(c.Term, sc.Name) {
			out = append(out, c)
		}
	}
	return out, nil
}
