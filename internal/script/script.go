// Copyright 2026 the Dubash contributors. Apache-2.0.

// Package script handles what the speaker plans to say: text extracted
// from an uploaded or pasted script, cut into chunks, searched, and
// softly aligned with the live speech. The script is context, never
// content: nothing in it is ever shown to the audience unless the
// speaker says it, and its text is data, never instructions.
package script

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxBytes bounds an uploaded script file.
const MaxBytes = 10 << 20

// ErrFormat is returned for a file type Extract cannot read.
var ErrFormat = errors.New("script: unsupported file type: upload .txt, .md or .docx, or paste the text (PDF is not supported yet)")

// Extract returns the plain text of a script file, by its name's
// extension: .txt and .md as UTF-8 text, .docx from its document XML.
func Extract(name string, data []byte) (string, error) {
	var text string
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".text", "":
		if !utf8.Valid(data) {
			return "", errors.New("script: the text file is not UTF-8")
		}
		text = string(data)
	case ".docx":
		t, err := docx(data)
		if err != nil {
			return "", err
		}
		text = t
	default:
		return "", ErrFormat
	}
	return Clean(text), nil
}

// Clean normalizes script text: NFC, one blank line between paragraphs,
// single spaces, no control characters.
func Clean(text string) string {
	text = norm.NFC.String(strings.ReplaceAll(text, "\r\n", "\n"))
	var paras []string
	for _, p := range strings.Split(text, "\n\n") {
		p = strings.Map(func(r rune) rune {
			if r == '\n' || r == '\t' {
				return ' '
			}
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, p)
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			paras = append(paras, p)
		}
	}
	return strings.Join(paras, "\n\n")
}

// docx reads the paragraphs of a Word document's word/document.xml.
func docx(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("script: not a .docx file: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return docxText(io.LimitReader(rc, 50<<20))
	}
	return "", errors.New("script: the .docx file has no document text")
}

func docxText(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	var b strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return b.String(), nil
		}
		if err != nil {
			return "", fmt.Errorf("script: reading .docx: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				b.WriteByte(' ')
			case "br":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				b.WriteString("\n\n")
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
}

// Chunk is a passage of a script, about a paragraph long: the unit the
// live speech is aligned with.
type Chunk struct {
	// Doc names the script; Index is the chunk's position across all of an
	// event's scripts, in order.
	Doc   string `json:"doc"`
	Index int    `json:"index"`
	Text  string `json:"text"`
}

// Chunk sizes, in words.
const (
	targetWords = 60
	maxWords    = 100
)

// Split cuts a script into chunks of about targetWords, at paragraph and
// then sentence boundaries.
func Split(doc, text string, first int) []Chunk {
	var out []Chunk
	var cur []string
	n := 0
	flush := func() {
		if len(cur) > 0 {
			out = append(out, Chunk{Doc: doc, Index: first + len(out), Text: strings.Join(cur, " ")})
			cur, n = nil, 0
		}
	}
	for _, para := range strings.Split(text, "\n\n") {
		for _, sent := range sentences(para) {
			w := len(strings.Fields(sent))
			if n > 0 && n+w > maxWords {
				flush()
			}
			cur = append(cur, sent)
			n += w
			if n >= targetWords {
				flush()
			}
		}
		if n >= targetWords/2 {
			flush()
		}
	}
	flush()
	return out
}

// sentences splits a paragraph after . ? ! । ॥ followed by a space.
func sentences(p string) []string {
	var out []string
	start := 0
	rs := []rune(p)
	for i, r := range rs {
		if strings.ContainsRune(".?!।॥", r) && (i+1 == len(rs) || rs[i+1] == ' ') {
			if s := strings.TrimSpace(string(rs[start : i+1])); s != "" {
				out = append(out, s)
			}
			start = i + 1
		}
	}
	if s := strings.TrimSpace(string(rs[start:])); s != "" {
		out = append(out, s)
	}
	return out
}
