// Package store persists finalized segments as one append-only JSONL file
// per room, so transcripts survive restarts and can be exported.
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

	"github.com/UtsavInTech/dubash/internal/caption"
)

// Store writes segments under dir/{room}.jsonl.
type Store struct {
	dir string
	mu  sync.Mutex
}

// New creates dir if needed.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	return &Store{dir: dir}, nil
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
	f, err := os.OpenFile(s.path(seg.Room), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open transcript: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("write transcript: %w", err)
	}
	return f.Close()
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
			return segs, skipped, nil
		}
		if err != nil {
			return nil, 0, fmt.Errorf("read transcript: %w", err)
		}
	}
}
