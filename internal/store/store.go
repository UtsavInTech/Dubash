// Package store persists finalized segments as one append-only JSONL file
// per room, so transcripts survive restarts and can be exported.
//
// Modified by the Dubash contributors: a corrected line is appended again
// and Load keeps its latest revision.
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/UtsavInTech/dubash/internal/caption"
)

// Store writes segments under dir/{room}.jsonl. Transcripts are what
// people said: the directory and files are readable by the server's user
// only.
type Store struct {
	dir string
	mu  sync.Mutex
	off map[string]bool // rooms whose transcript is not kept
}

// New creates dir if needed.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	// Tighten a directory created by an older version.
	os.Chmod(dir, 0o700)
	return &Store{dir: dir, off: map[string]bool{}}, nil
}

// Disable stops keeping room's transcript (store_transcript: false):
// Append drops its lines, and its captions live only in memory.
func (s *Store) Disable(room string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.off[room] = true
}

// Delete removes room's transcript.
func (s *Store) Delete(room string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(room)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete transcript: %w", err)
	}
	return nil
}

// Purge drops room's lines transcribed before cutoff, rewriting the file
// atomically, and returns how many it dropped. Lines without a time
// (written by old versions) are kept.
func (s *Store) Purge(room string, cutoff time.Time) (int, error) {
	segs, _, err := s.Load(room)
	if err != nil || len(segs) == 0 {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var keep bytes.Buffer
	dropped := 0
	for _, seg := range segs {
		if !seg.At.IsZero() && seg.At.Before(cutoff) {
			dropped++
			continue
		}
		line, err := json.Marshal(seg)
		if err != nil {
			return 0, fmt.Errorf("encode segment: %w", err)
		}
		keep.Write(append(line, '\n'))
	}
	if dropped == 0 {
		return 0, nil
	}
	if keep.Len() == 0 {
		return dropped, os.Remove(s.path(room))
	}
	tmp := s.path(room) + ".tmp"
	if err := os.WriteFile(tmp, keep.Bytes(), 0o600); err != nil {
		return 0, fmt.Errorf("purge transcript: %w", err)
	}
	if err := os.Rename(tmp, s.path(room)); err != nil {
		return 0, fmt.Errorf("purge transcript: %w", err)
	}
	return dropped, nil
}

func (s *Store) path(room string) string {
	return filepath.Join(s.dir, room+".jsonl")
}

// Append adds one segment to its room's file.
func (s *Store) Append(seg caption.Segment) error {
	line, err := json.Marshal(seg)
	if err != nil {
		return fmt.Errorf("encode segment: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.off[seg.Room] {
		return nil
	}
	f, err := os.OpenFile(s.path(seg.Room), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("write transcript: %w", err)
	}
	return f.Close()
}

// latest keeps, for each segment id, its last stored revision (a live
// correction appends the line again), in the order the ids first appear.
func latest(segs []caption.Segment) []caption.Segment {
	pos := map[int]int{}
	var out []caption.Segment
	for _, s := range segs {
		if i, ok := pos[s.ID]; ok {
			if s.Rev >= out[i].Rev {
				out[i] = s
			}
			continue
		}
		pos[s.ID] = len(out)
		out = append(out, s)
	}
	return out
}

// Load returns every decodable segment of room, oldest first, and how
// many lines it skipped because they could not be decoded (a torn write
// after a crash, a hand edit). A room with no transcript yet returns nil,
// 0, nil. Only I/O failures are errors, so one bad line never keeps the
// server from starting.
func (s *Store) Load(room string) (segs []caption.Segment, skipped int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path(room))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	rd := bufio.NewReader(f)
	for {
		line, err := rd.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var seg caption.Segment
			if json.Unmarshal(line, &seg) == nil {
				segs = append(segs, seg)
			} else {
				skipped++
			}
		}
		if errors.Is(err, io.EOF) {
			return latest(segs), skipped, nil
		}
		if err != nil {
			return nil, 0, fmt.Errorf("read transcript: %w", err)
		}
	}
}
