// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasBuiltinSigil(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{name: "builtin", line: ":restart", want: true},
		{name: "builtin with arguments", line: ":history delete 2", want: true},
		{name: "leading space", line: "  :restart", want: true},
		{name: "instance command", line: "restart", want: false},
		{name: "instance command that mentions a builtin", line: "ls && :mount v /mnt", want: false},
		// ":" on its own, and ":" as a command word, are the POSIX null
		// command rather than a sigil, so they belong to the instance.
		{name: "null command", line: ":", want: false},
		{name: "null command with arguments", line: ": foo", want: false},
		{name: "colon inside a word", line: "PATH=/a:/b", want: false},
		{name: "empty line", line: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, HasBuiltinSigil(tt.line))
		})
	}
}

func TestHighlightShellLineBuiltins(t *testing.T) {
	builtins := map[string]bool{"restart": true, "history": true}

	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "sigil is part of the builtin word",
			line: ":restart",
			want: shellHighlightBuiltinStyle.Render(":restart"),
		},
		{
			// Without the sigil the instance runs it, so it is coloured as
			// one of the instance's commands even though a builtin of that
			// name exists.
			name: "bare builtin name is an instance command",
			line: "restart",
			want: shellHighlightCmdStyle.Render("restart"),
		},
		{
			name: "instance command",
			line: "ls",
			want: shellHighlightCmdStyle.Render("ls"),
		},
		{
			name: "sigil on a non-builtin does not promote it",
			line: ":nope",
			want: shellHighlightCmdStyle.Render(":nope"),
		},
		{
			name: "only the command word is styled",
			line: ":restart extra",
			want: shellHighlightBuiltinStyle.Render(":restart") + " extra",
		},
		{
			// The shell dispatches on the whole line, so only its first
			// word can be a builtin - this one goes to the instance entire.
			name: "sigil after an operator is not a builtin",
			line: "ls && :history",
			want: shellHighlightCmdStyle.Render("ls") +
				shellHighlightOpStyle.Render(" && ") +
				shellHighlightCmdStyle.Render(":history"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, highlightShellLine(tt.line, builtins))
		})
	}

	// Without a builtin set nothing is promoted, which is what history
	// rendering falls back to before the set is wired up.
	assert.Equal(t, shellHighlightCmdStyle.Render(":restart"), highlightShellLine(":restart", nil))
}

func TestSandboxCompleterSigil(t *testing.T) {
	c := NewSandboxCompleter([]CompletionNode{
		{Name: "restart"},
		{Name: "history", Children: []CompletionNode{{Name: "list"}, {Name: "delete"}}},
	})

	// readline's completer emits the remainder of the word with a trailing
	// space, ready to be inserted at the cursor.
	candidates := func(line string) []string {
		out, _ := c.Do([]rune(line), len([]rune(line)))
		got := make([]string, 0, len(out))
		for _, r := range out {
			got = append(got, string(r))
		}
		return got
	}

	t.Run("sigil completes builtins", func(t *testing.T) {
		assert.Equal(t, []string{"start "}, candidates(":re"))
	})

	t.Run("sigil completes subcommands", func(t *testing.T) {
		assert.Equal(t, []string{"ist "}, candidates(":history l"))
	})

	t.Run("bare sigil lists every builtin and nothing else", func(t *testing.T) {
		got := candidates(":")
		assert.ElementsMatch(t, []string{"restart ", "history "}, got)
	})

	t.Run("the two namespaces don't cross", func(t *testing.T) {
		// "ls" is one of the fallback instance commands, and no builtin
		// starts with an l, so each tree answers only for its own side.
		assert.Empty(t, candidates(":l"))
		assert.Equal(t, []string{"s "}, candidates("l"))

		// A bare word is the instance's even where a builtin shares the
		// name, so it isn't offered without the sigil.
		assert.Empty(t, candidates("re"))
	})

	t.Run("cursor before the sigil", func(t *testing.T) {
		out, offset := c.Do([]rune(":restart"), 0)
		require.Empty(t, out)
		assert.Zero(t, offset)
	})
}
