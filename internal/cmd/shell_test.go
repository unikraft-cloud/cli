// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellBuiltinsDerivedFromGrammar(t *testing.T) {
	b, err := newShellBuiltins(io.Discard, io.Discard)
	require.NoError(t, err)

	menuNames := func() []string {
		var out []string
		for _, entry := range b.menu {
			out = append(out, entry.name)
		}
		return out
	}()

	completionNames := func() []string {
		var out []string
		for _, node := range b.completion {
			out = append(out, node.Name)
		}
		return out
	}()

	t.Run("every grammar command reaches help and completion", func(t *testing.T) {
		for _, child := range b.parser.Model.Children {
			if child.Type != kong.CommandNode || child.Hidden {
				continue
			}
			assert.True(t, b.names[child.Name], "%s missing from parser names", child.Name)
			assert.Contains(t, menuNames, child.Name)
			assert.Contains(t, completionNames, child.Name)
		}
	})

	t.Run("intrinsics reach help and completion but never the parser", func(t *testing.T) {
		for _, entry := range slices.Concat(shellIntrinsicsHead, shellIntrinsicsTail) {
			assert.Contains(t, menuNames, entry.name)
			assert.Contains(t, completionNames, entry.name)
			// handleBuiltin and the shell loop answer these directly. Adding
			// them to names would hand them to kong, which has no grammar
			// for them, so every one would fail to parse.
			assert.False(t, b.names[entry.name], "%s must not be parsed by kong", entry.name)
		}
	})

	t.Run("all covers exactly the top-level builtins", func(t *testing.T) {
		want := append([]string{}, completionNames...)
		got := make([]string, 0, len(b.all))
		for name := range b.all {
			got = append(got, name)
		}
		assert.ElementsMatch(t, want, got)

		// Subcommands are not commands in their own right: a line starting
		// with "list" is the instance's, not ours.
		assert.False(t, b.all["list"])
		assert.False(t, b.all["env"])
	})

	t.Run("help lines carry the sigil and describe their arguments", func(t *testing.T) {
		usage := map[string]string{}
		for _, entry := range b.menu {
			usage[entry.usage] = entry.desc
		}
		assert.Contains(t, usage, ":history rerun <index>")
		assert.Contains(t, usage, ":mount <volume> <path> [<mode>]")
		// A command whose bare form runs a default subcommand says so.
		assert.Contains(t, usage[":volumes"], "alias for :volumes mounted")
	})

	t.Run("help descriptions are sentence style", func(t *testing.T) {
		for _, entry := range b.menu {
			require.NotEmpty(t, entry.desc, "%s has no description", entry.usage)
			assert.Equal(t, strings.ToLower(entry.desc[:1]), entry.desc[:1],
				"%q should start lowercase", entry.desc)
		}
	})
}

func TestBuiltinFields(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "builtin",
			line: ":restart",
			want: []string{"restart"},
		},
		{
			name: "builtin with arguments",
			line: ":history delete 2",
			want: []string{"history", "delete", "2"},
		},
		{
			name: "quoted argument",
			line: `:cd "/srv/my app"`,
			want: []string{"cd", "/srv/my app"},
		},
		{
			name: "assignment as an argument",
			line: ":export KEY=value",
			want: []string{"export", "KEY=value"},
		},
		{
			name: "leading whitespace",
			line: "   :get",
			want: []string{"get"},
		},

		// A builtin runs in the CLI, not on the instance, so there is
		// nothing here to compose it with. These are refused whole rather
		// than half-honoured.
		{name: "chained with &&", line: ":mount vol /mnt && ls"},
		{name: "chained with ;", line: ":get; ls"},
		{name: "piped", line: ":get | grep memory"},
		{name: "redirected", line: ":get > out.txt"},
		{name: "backgrounded", line: ":restart &"},
		{name: "negated", line: "! :get"},
		{name: "subshell", line: "( :get )"},
		{name: "command substitution in an argument", line: ":cd $(pwd)"},
		{name: "loop", line: "for i in 1 2; do :get; done"},

		// Nothing to dispatch on.
		{name: "no sigil", line: "restart"},
		{name: "null command", line: ":"},
		{name: "empty line", line: ""},
		{name: "unparseable", line: `:get "unclosed`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := builtinFields(tt.line)
			assert.Equal(t, tt.want != nil, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
