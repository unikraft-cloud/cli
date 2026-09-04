// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/reeflective/readline"
	"mvdan.cc/sh/v3/syntax"
)

// prompt is the line editor the session reads from; the editing itself belongs to readline.
type prompt struct {
	rl      *readline.Shell
	history *sessionHistory
}

func (s *session) newPrompt(ctx context.Context) *prompt {
	rl := readline.NewShell()
	history := &sessionHistory{}

	rl.History.Add("session", history)
	rl.Prompt.Primary(func() string { return s.prompt(false) })
	rl.Prompt.Secondary(func() string { return s.prompt(true) })

	rl.AcceptMultiline = acceptMultiline
	rl.SyntaxHighlighter = func(line []rune) string {
		return highlight(string(line), s.isBuiltinName)
	}
	rl.Completer = s.completer(ctx)

	return &prompt{rl: rl, history: history}
}

func (p *prompt) readLine() (string, error) { return p.rl.Readline() }

func acceptMultiline(line []rune) bool {
	_, err := syntax.NewParser().Parse(strings.NewReader(string(line)), "")
	return !syntax.IsIncomplete(err)
}

func (s *session) isBuiltinName(name string) bool {
	return slices.Contains(s.builtinNames(), name)
}

// sessionHistory is what the session has run, kept for as long as it lasts.
type sessionHistory struct {
	mu    sync.RWMutex
	lines []string
}

func (h *sessionHistory) Write(line string) (int, error) {
	if line = strings.TrimSpace(line); line == "" {
		return h.Len(), nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if n := len(h.lines); n == 0 || h.lines[n-1] != line {
		h.lines = append(h.lines, line)
	}
	return len(h.lines), nil
}

func (h *sessionHistory) GetLine(pos int) (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if pos < 0 || pos >= len(h.lines) {
		return "", nil
	}
	return h.lines[pos], nil
}

func (h *sessionHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.lines)
}

func (h *sessionHistory) Dump() any { return h.recalled() }

func (h *sessionHistory) recalled() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return slices.Clone(h.lines)
}
