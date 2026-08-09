// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

// TerminalWriter removes the visible flicker of the readline prompt.
//
// On every keystroke readline redraws the whole input line, and it does so
// with two separate writes to the terminal: first an erase sequence
// ("\033[J\033[2K\r", blanking the line), then the prompt plus the freshly
// painted line. Between those two writes the line is genuinely empty, so a
// terminal refresh landing in that window shows a blank line — that is the
// flicker, and it gets worse the faster you type.
//
// TerminalWriter holds erase-only writes back and emits them together with
// the redraw that follows, as a single write wrapped in DEC mode 2026
// (synchronized output) so terminals supporting it never present a frame in
// between. Terminals that don't support the mode ignore it, and still
// benefit from the erase and the redraw arriving as one write.

import (
	"bytes"
	"io"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

type TerminalWriter struct {
	mu      sync.Mutex
	w       io.Writer
	pending []byte
}

func NewTerminalWriter(w io.Writer) *TerminalWriter {
	return &TerminalWriter{w: w}
}

func (t *TerminalWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if isEraseOnly(p) {
		t.pending = append(t.pending, p...)
		return len(p), nil
	}

	if len(t.pending) == 0 {
		return t.w.Write(p)
	}

	var buf bytes.Buffer
	buf.Grow(len(t.pending) + len(p) + 16)
	buf.WriteString(ansi.SetModeSynchronizedOutput)
	buf.Write(t.pending)
	buf.Write(p)
	buf.WriteString(ansi.ResetModeSynchronizedOutput)
	t.pending = t.pending[:0]

	if _, err := t.w.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush emits any erase sequence still held back. readline erases the line
// without redrawing it in a few places (EOF on an empty line, for one), so
// the shell loop calls this whenever it takes the terminal back from
// readline to make sure a held erase can never land on unrelated output.
func (t *TerminalWriter) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.pending) == 0 {
		return nil
	}

	_, err := t.w.Write(t.pending)
	t.pending = t.pending[:0]
	return err
}

// The sequences readline emits to blank the line before redrawing it. These
// are matched against, not written, so they're the literal bytes readline
// produces rather than anything this package composes.
var (
	eraseBelow = []byte(ansi.EraseScreenBelow)
	eraseLine  = []byte(ansi.EraseEntireLine)
	cursorUp   = []byte(ansi.CUU1)
)

// isEraseOnly reports whether p consists purely of the cursor and erase
// sequences readline uses to blank the current line, with nothing drawn.
// Such a write is never useful on its own — it only ever precedes a redraw.
// Anything else, including full screen clears on ^L and cursor position
// queries, is passed through untouched.
func isEraseOnly(p []byte) bool {
	erases := false

	for i := 0; i < len(p); {
		rest := p[i:]
		switch {
		case rest[0] == '\r' || rest[0] == '\b':
			i++
		case bytes.HasPrefix(rest, eraseBelow):
			erases = true
			i += len(eraseBelow)
		case bytes.HasPrefix(rest, eraseLine):
			erases = true
			i += len(eraseLine)
		case bytes.HasPrefix(rest, cursorUp):
			i += len(cursorUp)
		default:
			return false
		}
	}

	return erases
}
