// Copyright 2026 the Dubash contributors. Apache-2.0.

package bank

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UtsavInTech/dubash/internal/script"
)

//go:embed migrations/*.sql
var migrations embed.FS

// PGStore keeps the bank in PostgreSQL.
type PGStore struct {
	db *pgxpool.Pool
}

// OpenPG connects to url and applies pending migrations.
func OpenPG(ctx context.Context, url string) (*PGStore, error) {
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("bank: %w", err)
	}
	s := &PGStore{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// migrate applies the embedded migrations not yet in schema_migrations,
// each in its own transaction, under an advisory lock so two servers
// starting together do not race.
func (s *PGStore) migrate(ctx context.Context) error {
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("bank: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(7261)`); err != nil {
		return fmt.Errorf("bank: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(7261)`)
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version int PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("bank: %w", err)
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	for _, name := range names {
		v, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(name, "migrations/"), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bank: migration %s: %w", name, err)
		}
		var done bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, v).Scan(&done); err != nil {
			return fmt.Errorf("bank: %w", err)
		}
		if done {
			continue
		}
		sql, _ := migrations.ReadFile(name)
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("bank: migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, v); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Close implements Store.
func (s *PGStore) Close() error {
	s.db.Close()
	return nil
}

const entryColumns = `e.row_id, e.entry_id, e.version, e.scope, e.scope_id, e.term, e.lang, e.aliases, e.misheard,
	e.pronunciation, e.meaning, e.keep, e.ambiguous, e.status, e.source, e.note, e.created_by, e.updated_at,
	COALESCE((SELECT jsonb_object_agg(t.lang, t.text) FROM vocabulary_translations t WHERE t.row_id = e.row_id), '{}')`

func scanEntry(row pgx.Row) (Entry, error) {
	var e Entry
	var rowID int64
	var aliases, misheard, translations []byte
	var scope, status string
	err := row.Scan(&rowID, &e.ID, &e.Version, &scope, &e.ScopeID, &e.Term, &e.Lang, &aliases, &misheard,
		&e.Pronunciation, &e.Meaning, &e.Keep, &e.Ambiguous, &status, &e.Source, &e.Note, &e.CreatedBy, &e.UpdatedAt, &translations)
	if err != nil {
		return Entry{}, err
	}
	e.Scope, e.Status = Scope(scope), Status(status)
	json.Unmarshal(aliases, &e.Aliases)
	json.Unmarshal(misheard, &e.Misheard)
	json.Unmarshal(translations, &e.Translations)
	if len(e.Translations) == 0 {
		e.Translations = nil
	}
	e.UpdatedAt = e.UpdatedAt.UTC()
	return e, nil
}

func (s *PGStore) queryEntries(ctx context.Context, where string, args ...any) ([]Entry, error) {
	rows, err := s.db.Query(ctx, `SELECT `+entryColumns+` FROM vocabulary_entries e WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("bank: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("bank: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Entries implements Store.
func (s *PGStore) Entries(ctx context.Context, f Filter) ([]Entry, error) {
	where := []string{"e.current"}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.Scope != "" {
		add("e.scope = $%d", string(f.Scope))
	}
	if f.ScopeID != "" {
		add("e.scope_id = $%d", f.ScopeID)
	}
	if f.Status != "" {
		add("e.status = $%d", string(f.Status))
	}
	es, err := s.queryEntries(ctx, strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	if f.Query != "" { // folding (nukta, NFC) is done in Go, as for FileStore
		es = slices.DeleteFunc(es, func(e Entry) bool { return !f.match(e) })
	}
	sortEntries(es)
	return es, nil
}

// Entry implements Store.
func (s *PGStore) Entry(ctx context.Context, id string) (Entry, error) {
	es, err := s.queryEntries(ctx, "e.current AND e.entry_id = $1", id)
	if err != nil {
		return Entry{}, err
	}
	if len(es) == 0 {
		return Entry{}, ErrNotFound
	}
	return es[0], nil
}

// History implements Store.
func (s *PGStore) History(ctx context.Context, id string) ([]Entry, error) {
	es, err := s.queryEntries(ctx, "e.entry_id = $1 ORDER BY e.version", id)
	if err != nil {
		return nil, err
	}
	if len(es) == 0 {
		return nil, ErrNotFound
	}
	return es, nil
}

func (s *PGStore) audit(ctx context.Context, tx pgx.Tx, actor, action, object, detail string) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_log (actor, action, object, detail) VALUES ($1, $2, $3, $4)`, actor, action, object, detail)
	return err
}

// SaveEntry implements Store.
func (s *PGStore) SaveEntry(ctx context.Context, e Entry, actor string) (Entry, error) {
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Entry{}, fmt.Errorf("bank: %w", err)
	}
	defer tx.Rollback(ctx)
	action := "create"
	if e.ID == "" {
		e.ID, e.Version = newID(), 1
	} else {
		var v int
		err := tx.QueryRow(ctx, `SELECT version FROM vocabulary_entries WHERE entry_id = $1 AND current FOR UPDATE`, e.ID).Scan(&v)
		if errors.Is(err, pgx.ErrNoRows) {
			return Entry{}, ErrNotFound
		}
		if err != nil {
			return Entry{}, fmt.Errorf("bank: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE vocabulary_entries SET current = false WHERE entry_id = $1 AND current`, e.ID); err != nil {
			return Entry{}, fmt.Errorf("bank: %w", err)
		}
		e.Version = v + 1
		action = "update"
	}
	e.CreatedBy, e.UpdatedAt = actor, time.Now().UTC().Truncate(time.Microsecond)
	aliases, _ := json.Marshal(orEmpty(e.Aliases))
	misheard, _ := json.Marshal(orEmpty(e.Misheard))
	var rowID int64
	err = tx.QueryRow(ctx, `INSERT INTO vocabulary_entries
		(entry_id, version, scope, scope_id, term, lang, aliases, misheard, pronunciation, meaning, keep, ambiguous, status, source, note, created_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING row_id`,
		e.ID, e.Version, string(e.Scope), e.ScopeID, e.Term, e.Lang, aliases, misheard, e.Pronunciation, e.Meaning,
		e.Keep, e.Ambiguous, string(e.Status), e.Source, e.Note, e.CreatedBy, e.UpdatedAt).Scan(&rowID)
	if err != nil {
		return Entry{}, fmt.Errorf("bank: %w", err)
	}
	for l, t := range e.Translations {
		if _, err := tx.Exec(ctx, `INSERT INTO vocabulary_translations (row_id, lang, text) VALUES ($1, $2, $3)`, rowID, l, t); err != nil {
			return Entry{}, fmt.Errorf("bank: %w", err)
		}
	}
	if err := s.audit(ctx, tx, actor, action, "entry:"+e.ID, fmt.Sprintf("v%d %s %q", e.Version, e.Status, e.Term)); err != nil {
		return Entry{}, fmt.Errorf("bank: %w", err)
	}
	return e, tx.Commit(ctx)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// SaveVersion implements Store.
func (s *PGStore) SaveVersion(ctx context.Context, v Version, actor string) (Version, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("bank: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "version:"+v.Event); err != nil {
		return Version{}, fmt.Errorf("bank: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM event_context_versions WHERE event_id = $1`, v.Event).Scan(&v.Number); err != nil {
		return Version{}, fmt.Errorf("bank: %w", err)
	}
	v.CreatedBy = actor
	v.CreatedAt = v.CreatedAt.UTC().Truncate(time.Microsecond)
	entries, _ := json.Marshal(v.Entries)
	chunks, _ := json.Marshal(orEmptyChunks(v.Script))
	err = tx.QueryRow(ctx, `INSERT INTO event_context_versions (event_id, number, created_at, created_by, note, speaker, hash, entries, script)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		v.Event, v.Number, v.CreatedAt, v.CreatedBy, v.Note, v.Speaker, v.Hash, entries, chunks).Scan(&v.ID)
	if err != nil {
		return Version{}, fmt.Errorf("bank: %w", err)
	}
	if err := s.audit(ctx, tx, actor, "compile", "event:"+v.Event, fmt.Sprintf("version %d, %d entries, %s", v.Number, len(v.Entries), v.Hash)); err != nil {
		return Version{}, fmt.Errorf("bank: %w", err)
	}
	return v, tx.Commit(ctx)
}

const versionColumns = `id, event_id, number, created_at, created_by, note, speaker, hash`

func scanVersion(row pgx.Row, withEntries bool) (Version, error) {
	var v Version
	dest := []any{&v.ID, &v.Event, &v.Number, &v.CreatedAt, &v.CreatedBy, &v.Note, &v.Speaker, &v.Hash}
	var entries, chunks []byte
	if withEntries {
		dest = append(dest, &entries, &chunks)
	}
	if err := row.Scan(dest...); err != nil {
		return Version{}, err
	}
	v.CreatedAt = v.CreatedAt.UTC()
	if withEntries {
		if err := json.Unmarshal(entries, &v.Entries); err != nil {
			return Version{}, err
		}
		if err := json.Unmarshal(chunks, &v.Script); err != nil {
			return Version{}, err
		}
		if len(v.Script) == 0 {
			v.Script = nil
		}
	}
	return v, nil
}

func orEmptyChunks(c []script.Chunk) []script.Chunk {
	if c == nil {
		return []script.Chunk{}
	}
	return c
}

// Version implements Store.
func (s *PGStore) Version(ctx context.Context, id int64) (Version, error) {
	v, err := scanVersion(s.db.QueryRow(ctx, `SELECT `+versionColumns+`, entries, script FROM event_context_versions WHERE id = $1`, id), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	return v, err
}

// Versions implements Store.
func (s *PGStore) Versions(ctx context.Context, event string) ([]Version, error) {
	rows, err := s.db.Query(ctx, `SELECT `+versionColumns+` FROM event_context_versions WHERE event_id = $1 ORDER BY number DESC`, event)
	if err != nil {
		return nil, fmt.Errorf("bank: %w", err)
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		v, err := scanVersion(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Pin implements Store.
func (s *PGStore) Pin(ctx context.Context, event string, id int64, actor string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bank: %w", err)
	}
	defer tx.Rollback(ctx)
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM event_context_versions WHERE id = $1 AND event_id = $2)`, id, event).Scan(&ok); err != nil {
		return fmt.Errorf("bank: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `INSERT INTO event_context_pins (event_id, version_id, pinned_by) VALUES ($1, $2, $3)
		ON CONFLICT (event_id) DO UPDATE SET version_id = EXCLUDED.version_id, pinned_at = now(), pinned_by = EXCLUDED.pinned_by`, event, id, actor); err != nil {
		return fmt.Errorf("bank: %w", err)
	}
	if err := s.audit(ctx, tx, actor, "pin", "event:"+event, fmt.Sprintf("version id %d", id)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Pinned implements Store.
func (s *PGStore) Pinned(ctx context.Context, event string) (Version, bool, error) {
	v, err := scanVersion(s.db.QueryRow(ctx, `SELECT `+versionColumns+`, entries, script FROM event_context_versions
		WHERE id = (SELECT version_id FROM event_context_pins WHERE event_id = $1)`, event), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, false, nil
	}
	if err != nil {
		return Version{}, false, fmt.Errorf("bank: %w", err)
	}
	return v, true, nil
}

// SaveCorrection implements Store.
func (s *PGStore) SaveCorrection(ctx context.Context, c Correction) (Correction, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Correction{}, fmt.Errorf("bank: %w", err)
	}
	defer tx.Rollback(ctx)
	sug, _ := json.Marshal(orEmpty(c.Suggested))
	c.At = c.At.UTC().Truncate(time.Microsecond)
	err = tx.QueryRow(ctx, `INSERT INTO transcript_corrections (event_id, segment_id, rev, raw, corrected, lang, at, by, suggested)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		c.Event, c.Segment, c.Rev, c.Raw, c.Corrected, c.Lang, c.At, c.By, sug).Scan(&c.ID)
	if err != nil {
		return Correction{}, fmt.Errorf("bank: %w", err)
	}
	if err := s.audit(ctx, tx, c.By, "correct", fmt.Sprintf("event:%s segment:%d", c.Event, c.Segment), fmt.Sprintf("%q → %q", c.Raw, c.Corrected)); err != nil {
		return Correction{}, err
	}
	return c, tx.Commit(ctx)
}

// Corrections implements Store.
func (s *PGStore) Corrections(ctx context.Context, event string) ([]Correction, error) {
	rows, err := s.db.Query(ctx, `SELECT id, event_id, segment_id, rev, raw, corrected, lang, at, by, suggested
		FROM transcript_corrections WHERE $1 = '' OR event_id = $1 ORDER BY id`, event)
	if err != nil {
		return nil, fmt.Errorf("bank: %w", err)
	}
	defer rows.Close()
	var out []Correction
	for rows.Next() {
		var c Correction
		var sug []byte
		if err := rows.Scan(&c.ID, &c.Event, &c.Segment, &c.Rev, &c.Raw, &c.Corrected, &c.Lang, &c.At, &c.By, &sug); err != nil {
			return nil, err
		}
		c.At = c.At.UTC()
		json.Unmarshal(sug, &c.Suggested)
		if len(c.Suggested) == 0 {
			c.Suggested = nil
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Audit implements Store.
func (s *PGStore) Audit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `SELECT at, actor, action, object, detail FROM audit_log ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("bank: %w", err)
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.At, &a.Actor, &a.Action, &a.Object, &a.Detail); err != nil {
			return nil, err
		}
		a.At = a.At.UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}
