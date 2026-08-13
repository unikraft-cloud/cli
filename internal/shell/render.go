// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

// terminalWriter removes the visible flicker of the readline prompt: it holds
// erase-only writes back and emits them with the redraw that follows, as one
// write wrapped in DEC mode 2026 (synchronized output). Terminals without the
// mode ignore it, and still benefit from erase and redraw arriving together.

import (
	"bytes"
	"io"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

type terminalWriter struct {
	mu        sync.Mutex
	w         io.Writer
	pending   []byte
	lastFrame []byte
}

func newTerminalWriter(w io.Writer) *terminalWriter {
	return &terminalWriter{w: w}
}

func (t *terminalWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if isEraseOnly(p) {
		t.pending = append(t.pending, p...)
		return len(p), nil
	}

	if len(t.pending) == 0 {
		t.lastFrame = t.lastFrame[:0]
		return t.w.Write(p)
	}
	if move, ok := cursorMoveOnly(t.lastFrame, p); ok {
		t.pending = t.pending[:0]
		if len(move) > 0 {
			if _, err := t.w.Write(move); err != nil {
				return 0, err
			}
		}
		t.lastFrame = append(t.lastFrame[:0], p...)
		return len(p), nil
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
	t.lastFrame = append(t.lastFrame[:0], p...)
	return len(p), nil
}

func cursorMoveOnly(prev, next []byte) ([]byte, bool) {
	if len(prev) == 0 || len(next) == 0 {
		return nil, false
	}

	prevBody, prevBack := splitTrailingBackspaces(prev)
	nextBody, nextBack := splitTrailingBackspaces(next)
	if prevBody == nil || nextBody == nil || !bytes.Equal(prevBody, nextBody) {
		return nil, false
	}

	switch {
	case nextBack > prevBack: // cursor moved left
		return bytes.Repeat([]byte{'\b'}, nextBack-prevBack), true
	case nextBack < prevBack: // cursor moved right
		return []byte(ansi.CursorForward(prevBack - nextBack)), true
	default:
		return nil, true
	}
}

func splitTrailingBackspaces(frame []byte) ([]byte, int) {
	if bytes.Contains(frame, cursorUp) {
		return nil, 0
	}
	end := len(frame)
	for end > 0 && frame[end-1] == '\b' {
		end--
	}
	return frame[:end], len(frame) - end
}

func (t *terminalWriter) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.pending) == 0 {
		return nil
	}

	_, err := t.w.Write(t.pending)
	t.pending = t.pending[:0]
	return err
}

var (
	eraseBelow = []byte(ansi.EraseScreenBelow)
	eraseLine  = []byte(ansi.EraseEntireLine)
	cursorUp   = []byte(ansi.CUU1)
)

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
