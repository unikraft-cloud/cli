// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func knownBuiltin(name string) bool { return name == "jobs" || name == "start" }

func TestHighlight(t *testing.T) {
	for _, tt := range []struct {
		name     string
		line     string
		coloured []string
	}{
		{"empty", "", nil},
		{"plain-command", "ls -la", nil},
		{"single-quoted", "echo 'a string'", []string{"'a string'"}},
		{"double-quoted", `echo "a string"`, []string{`"a string"`}},
		{"unterminated-quote", "echo 'half", []string{"'half"}},
		{"pipe-and-redirect", "a | b > c", []string{"|", ">"}},
		{"and-or", "a && b || c", []string{"&", "|"}},
		{"dollar", "echo $HOME", []string{"$"}},
		{"builtin", ":jobs", []string{":jobs"}},
		{"builtin-with-arguments", ":start now", []string{":start"}},
		{"builtin-after-spaces", "   :jobs", []string{":jobs"}},
		{"builtin-after-a-tab", "\t:jobs", []string{":jobs"}},
		{"builtin-mid-line", "x :jobs", []string{":jobs"}},
		{"specials-inside-a-quote-stay-plain", "echo '| & ;'", []string{"'| & ;'"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := highlight(tt.line, knownBuiltin)

			assert.Equal(t, tt.line, ansi.Strip(got), "the text must survive unchanged")
			if len(tt.coloured) == 0 {
				assert.Equal(t, tt.line, got, "nothing to colour, so nothing added")
				return
			}
			for _, want := range tt.coloured {
				at := strings.Index(got, want)
				require.GreaterOrEqual(t, at, 0, "%q missing entirely", want)
				assert.True(t, at > 0 && got[at-1] == 'm', "%q is not coloured, in %q", want, got)
			}
		})
	}
}
