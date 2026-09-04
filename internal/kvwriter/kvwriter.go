// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kvwriter

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	xio "unikraft.com/x/io"
)

type keyValueWriter struct {
	w      io.Writer
	buffer []byte

	cells []cell

	cut             int
	separators      []string
	alignSeparators bool
}

type cell struct {
	key      []byte
	cleankey []byte
	value    []byte
	split    []byte
}

func (entry cell) width() int {
	return ansi.StringWidth(string(entry.cleankey)) + ansi.StringWidth(string(entry.split))
}

type KeyValueOpt func(*keyValueWriter)

func WithSeparator(splits ...string) KeyValueOpt {
	return func(b *keyValueWriter) {
		b.separators = splits
	}
}

func WithAlignedSeparator() KeyValueOpt {
	return func(b *keyValueWriter) {
		b.alignSeparators = true
	}
}

func WithIndent(indent string) KeyValueOpt {
	return func(b *keyValueWriter) {
		b.cut = len(indent)
	}
}

// KeyValueWriter returns a Writer that formats key-value pairs written to it.
// Each line written to the Writer should be in the format "key: value".
//
// The keys will be aligned based on the longest key, and styling rules are
// applied to them based on their leading whitespace after the specified indent
// is removed.
func KeyValueWriter(w io.Writer, opts ...KeyValueOpt) xio.WriteFlusher {
	kv := &keyValueWriter{w: w}
	for _, opt := range opts {
		opt(kv)
	}
	if len(kv.separators) == 0 {
		kv.separators = []string{":"}
	}
	return kv
}

func (b *keyValueWriter) Write(p []byte) (n int, err error) {
	b.buffer = append(b.buffer, p...)

	lastNewline := bytes.LastIndexByte(b.buffer, '\n')
	if lastNewline == -1 {
		return len(p), nil
	}

	lines := bytes.Lines(b.buffer[:lastNewline+1])
	b.buffer = b.buffer[lastNewline+1:]

	for line := range lines {
		b.cells = append(b.cells, b.parseLine(line))
	}
	return len(p), nil
}

func (b *keyValueWriter) parseLine(line []byte) cell {
	parsed, ok := b.splitLine(line)
	if ok {
		return parsed
	}
	return cell{value: line}
}

func (b *keyValueWriter) splitLine(line []byte) (cell, bool) {
	for _, split := range b.separators {
		if split == "" {
			continue
		}
		splitBytes := []byte(split)
		key, value, ok := bytes.Cut(line, splitBytes)
		if !ok {
			continue
		}
		if len(value) != 0 && !slices.Contains([]byte(" \n"), value[0]) {
			continue
		}
		key = bytes.TrimRightFunc(key, unicode.IsSpace)
		cleankey := []byte(ansi.Strip(string(key)))
		return cell{
			key:      key,
			cleankey: cleankey,
			value:    bytes.TrimSpace(value),
			split:    splitBytes,
		}, true
	}
	return cell{}, false
}

func (b *keyValueWriter) flush() error {
	if len(b.buffer) > 0 {
		_, err := b.Write([]byte{'\n'})
		if err != nil {
			return err
		}
	}

	color := lipgloss.Writer.Profile != colorprofile.NoTTY

	maxKeyLen := 0
	for _, entry := range b.cells {
		if entry.key == nil {
			continue
		}
		maxKeyLen = max(maxKeyLen, entry.width())
	}

	for _, entry := range b.cells {
		if entry.key == nil {
			if _, err := fmt.Fprint(b.w, string(entry.value)); err != nil {
				return err
			}
			continue
		}
		key := entry.key
		cleankey := entry.cleankey
		if b.alignSeparators {
			key = bytes.TrimRight(key, " ")
			cleankey = bytes.TrimRight(cleankey, " ")
		}
		if len(cleankey) > 0 {
			var styleSeq, resetSeq ansi.Style
			if color {
				// NOTE: would be nice to use lipgloss styles here, but lipgloss
				// makes liberal use of full reset codes which can mess up if the
				// target text contains it's own styles.
				if b.cut < len(cleankey) && slices.Contains([]byte(" \t-"), cleankey[b.cut]) {
					// italic
					styleSeq = ansi.NewStyle(ansi.AttrItalic)
					resetSeq = ansi.NewStyle(ansi.AttrNoItalic)
				} else {
					// bold
					styleSeq = ansi.NewStyle(ansi.AttrBold)
					resetSeq = ansi.NewStyle(ansi.AttrNormalIntensity)
				}
			}

			padding := max(0, maxKeyLen-(ansi.StringWidth(string(cleankey))+ansi.StringWidth(string(entry.split)))+1)
			line := []byte{}
			if styleSeq != nil {
				line = append(line, []byte(styleSeq.String())...)
			}
			line = append(line, key...)
			if styleSeq != nil {
				line = append(line, []byte(resetSeq.String())...)
			}
			if b.alignSeparators {
				line = append(line, bytes.Repeat([]byte(" "), padding)...)
				line = append(line, entry.split...)
				if len(entry.value) > 0 {
					line = append(line, ' ')
				}
			} else {
				line = append(line, entry.split...)
				if len(entry.value) > 0 {
					line = append(line, bytes.Repeat([]byte(" "), padding)...)
				}
			}
			if len(entry.value) > 0 {
				line = append(line, entry.value...)
			}
			line = append(line, '\n')
			if _, err := b.w.Write(line); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *keyValueWriter) Flush() error {
	if err := b.flush(); err != nil {
		return err
	}
	if len(b.buffer) > 0 {
		if _, err := b.w.Write(b.buffer); err != nil {
			return err
		}
		b.buffer = nil
	}
	if tw, ok := b.w.(xio.Flusher); ok {
		return tw.Flush()
	}
	return nil
}
