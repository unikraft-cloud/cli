// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"cmp"
	"context"
	"path"
	"slices"
	"strings"

	"github.com/reeflective/readline"
)

const wordBreaks = " \t|;&(<>"

func (s *session) completer(ctx context.Context) func(line []rune, cursor int) readline.Completions {
	return func(line []rune, cursor int) readline.Completions {
		head := string(line[:min(cursor, len(line))])
		start := strings.LastIndexAny(head, wordBreaks) + 1
		word := head[start:]

		var matches []string
		if strings.TrimSpace(head[:start]) == "" && !strings.Contains(word, "/") {
			matches = s.commandMatches(ctx, word)
		} else {
			matches = s.pathMatches(ctx, word)
		}
		if len(matches) == 0 {
			return readline.Completions{}
		}

		comps := readline.CompleteValues(matches...).NoSpace('/')
		comps.PREFIX = word
		return comps
	}
}

func (s *session) pathMatches(ctx context.Context, word string) []string {
	dir, prefix := path.Split(word)

	entries, err := s.readDir(ctx, s.dir(), cmp.Or(dir, "."))
	if err != nil {
		return nil
	}

	var matches []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		name := dir + e.Name()
		if e.IsDir() {
			name += "/"
		}
		matches = append(matches, name)
	}
	return matches
}

func (s *session) commandMatches(ctx context.Context, word string) []string {
	var matches []string
	for _, name := range s.builtinNames() {
		if b := BuiltinSigil + name; strings.HasPrefix(b, word) {
			matches = append(matches, b)
		}
	}
	for _, name := range s.remoteCommands(ctx) {
		if strings.HasPrefix(name, word) {
			matches = append(matches, name)
		}
	}
	return matches
}

func (s *session) remoteCommands(ctx context.Context) []string {
	if s.commands != nil {
		return s.commands
	}

	out, err := s.script(ctx, `IFS=:; for d in $PATH; do ls -1 "$d" 2>/dev/null; done`)
	if err != nil {
		return nil
	}

	commands := []string{}
	seen := map[string]bool{}
	for name := range strings.SplitSeq(out, "\n") {
		if name = strings.TrimSpace(name); name != "" && !seen[name] {
			seen[name] = true
			commands = append(commands, name)
		}
	}
	slices.Sort(commands)
	s.commands = commands
	return s.commands
}
