// Copyright 2026 the Dubash contributors. Apache-2.0.

package script

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Fold is the form words are compared in: lower case, NFC, Devanagari
// nukta dropped (the same rule as the Dictation Bank's).
func Fold(s string) string {
	s = strings.ReplaceAll(norm.NFD.String(strings.ToLower(s)), "़", "")
	return norm.NFC.String(s)
}

// Words splits text into folded words, punctuation dropped.
func Words(text string) []string {
	var out []string
	for _, w := range strings.Fields(text) {
		w = strings.TrimFunc(w, func(r rune) bool { return unicode.IsPunct(r) || r == '।' || r == '॥' })
		if w != "" {
			out = append(out, Fold(w))
		}
	}
	return out
}

// Index is a BM25 index over an event's script chunks, held in memory:
// the live path searches it without a database.
type Index struct {
	chunks []Chunk
	tf     []map[string]int
	lens   []int
	df     map[string]int
	avg    float64
}

// NewIndex indexes chunks.
func NewIndex(chunks []Chunk) *Index {
	ix := &Index{chunks: chunks, df: map[string]int{}}
	total := 0
	for _, c := range chunks {
		tf := map[string]int{}
		ws := Words(c.Text)
		for _, w := range ws {
			tf[w]++
		}
		for w := range tf {
			ix.df[w]++
		}
		ix.tf = append(ix.tf, tf)
		ix.lens = append(ix.lens, len(ws))
		total += len(ws)
	}
	if len(chunks) > 0 {
		ix.avg = float64(total) / float64(len(chunks))
	}
	return ix
}

// Len is the number of chunks.
func (ix *Index) Len() int {
	if ix == nil {
		return 0
	}
	return len(ix.chunks)
}

// Chunk returns chunk i.
func (ix *Index) Chunk(i int) Chunk { return ix.chunks[i] }

// Scores returns the BM25 score of every chunk for query.
func (ix *Index) Scores(query string) []float64 {
	const k1, b = 1.2, 0.75
	scores := make([]float64, len(ix.chunks))
	n := float64(len(ix.chunks))
	seen := map[string]bool{}
	for _, q := range Words(query) {
		if seen[q] {
			continue
		}
		seen[q] = true
		df := float64(ix.df[q])
		if df == 0 {
			continue
		}
		idf := math.Log(1 + (n-df+0.5)/(df+0.5))
		for i, tf := range ix.tf {
			f := float64(tf[q])
			if f == 0 {
				continue
			}
			scores[i] += idf * f * (k1 + 1) / (f + k1*(1-b+b*float64(ix.lens[i])/ix.avg))
		}
	}
	return scores
}

// Search returns the indexes of the best k chunks for query, best first,
// with a positive score.
func (ix *Index) Search(query string, k int) []int {
	scores := ix.Scores(query)
	var idx []int
	for i, s := range scores {
		if s > 0 {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return scores[idx[a]] > scores[idx[b]] })
	if len(idx) > k {
		idx = idx[:k]
	}
	return idx
}

// Aligner estimates where in the script the speaker is. It is a soft
// estimate: speakers skip, repeat, improvise and answer questions, so it
// moves only on clear evidence and says how sure it is.
type Aligner struct {
	ix   *Index
	pos  int // current chunk, -1 before any evidence
	conf float64
}

// NewAligner aligns against ix.
func NewAligner(ix *Index) *Aligner { return &Aligner{ix: ix, pos: -1} }

// Alignment thresholds.
const (
	minAlignScore = 1.5 // BM25 score a chunk needs to count as evidence
	minConfidence = 0.3 // margin over the runner-up to move
)

// Observe takes a committed line and returns the chunk the speaker is
// most likely in, and a confidence from 0 (no idea: do not use the
// script) to 1.
func (a *Aligner) Observe(line string) (int, float64) {
	if a == nil || a.ix.Len() == 0 {
		return -1, 0
	}
	scores := a.ix.Scores(line)
	weighted := make([]float64, len(scores))
	for i, s := range scores {
		prior := 0.5 // anywhere: speakers jump
		if a.pos >= 0 {
			switch d := i - a.pos; {
			case d >= 0 && d <= 2:
				prior = 1 // where they are, or just ahead
			case d == -1 || d == 3:
				prior = 0.8
			}
		} else {
			prior = 1
		}
		weighted[i] = s * prior
	}
	order := make([]int, len(weighted))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(x, y int) int {
		switch {
		case weighted[x] > weighted[y]:
			return -1
		case weighted[x] < weighted[y]:
			return 1
		}
		return 0
	})
	best := order[0]
	if scores[best] < minAlignScore {
		a.conf *= 0.7 // no evidence: trust the old position less
		return a.pos, a.conf
	}
	margin := 1.0
	if len(order) > 1 && weighted[best] > 0 {
		margin = (weighted[best] - weighted[order[1]]) / weighted[best]
	}
	if margin >= minConfidence || best == a.pos {
		a.pos, a.conf = best, min(1, 0.5+margin/2)
	} else {
		a.conf *= 0.7
	}
	return a.pos, a.conf
}

// Window returns the text of the chunk at pos and the next one: the
// script passage a line is likely from.
func (a *Aligner) Window(pos int) string {
	if a == nil || pos < 0 || pos >= a.ix.Len() {
		return ""
	}
	text := a.ix.Chunk(pos).Text
	if pos+1 < a.ix.Len() {
		text += " " + a.ix.Chunk(pos+1).Text
	}
	return text
}
