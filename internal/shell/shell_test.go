// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellBuiltinsDerivedFromGrammar(t *testing.T) {
	b, err := newBuiltins(&rootCmd{}, io.Discard, io.Discard)
	require.NoError(t, err)

	// The grammar is parsed a second time here, so what the builtins derived
	// from it is checked against the grammar itself rather than against
	// another view of the same derivation.
	parser, err := kong.New(&rootCmd{}, kong.Name(""), kong.NoDefaultHelp(), kong.Exit(func(int) {}))
	require.NoError(t, err)

	menuNames := func() []string {
		var out []string
		for _, entry := range b.Menu() {
			out = append(out, entry.Name)
		}
		return out
	}()

	completionNames := func() []string {
		var out []string
		for _, node := range b.Completion() {
			out = append(out, node.Name)
		}
		return out
	}()

	t.Run("every grammar command reaches help and completion", func(t *testing.T) {
		for _, child := range parser.Model.Children {
			if child.Type != kong.CommandNode || child.Hidden {
				continue
			}
			assert.True(t, b.HasCommand(child.Name), "%s missing from parser names", child.Name)
			assert.Contains(t, menuNames, child.Name)
			assert.Contains(t, completionNames, child.Name)
		}
	})

	t.Run("intrinsics reach help and completion but never the parser", func(t *testing.T) {
		for _, name := range []string{"cd", "export", "clear", "exit"} {
			assert.Contains(t, menuNames, name)
			assert.Contains(t, completionNames, name)
			assert.True(t, b.IsBuiltin(name), "%s must be recognised as a builtin", name)
			// handleBuiltin and the shell loop answer these directly. Handing
			// one to kong, which has no grammar for it, would only fail.
			assert.False(t, b.HasCommand(name), "%s must not be parsed by kong", name)
		}
	})

	t.Run("only top-level builtins are recognised", func(t *testing.T) {
		for _, name := range completionNames {
			assert.True(t, b.IsBuiltin(name), "%s should be recognised as a builtin", name)
		}

		// Subcommands are not commands in their own right: a line starting
		// with "list" is the instance's, not ours.
		assert.False(t, b.IsBuiltin("list"))
		assert.False(t, b.IsBuiltin("env"))
	})

	t.Run("help lines carry the sigil and describe their arguments", func(t *testing.T) {
		usage := map[string]string{}
		for _, entry := range b.Menu() {
			usage[entry.Usage] = entry.Desc
		}
		assert.Contains(t, usage, ":history rerun <index>")
		assert.Contains(t, usage, ":mount <volume> <path> [<mode>]")
		// A command whose bare form runs a default subcommand says so.
		assert.Contains(t, usage[":volumes"], "alias for :volumes mounted")
	})

	t.Run("help descriptions are sentence style", func(t *testing.T) {
		for _, entry := range b.Menu() {
			require.NotEmpty(t, entry.Desc, "%s has no description", entry.Usage)
			assert.Equal(t, strings.ToLower(entry.Desc[:1]), entry.Desc[:1],
				"%q should start lowercase", entry.Desc)
		}
	})
}
