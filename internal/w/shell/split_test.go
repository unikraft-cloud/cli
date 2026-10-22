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

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "plain", in: "--verbose --port=80", want: []string{"--verbose", "--port=80"}},
		{name: "quotes", in: `a 'b c' "d e" f\ g`, want: []string{"a", "b c", "d e", "f g"}},
		{name: "escapes in double quotes", in: `"a\"b" "c\$d"`, want: []string{`a"b`, "c$d"}},
		{name: "params", in: `echo $HOME "p=$PATH"`, want: []string{"echo", "$HOME", "p=$PATH"}},
		{name: "param defaults", in: `${X:-def} "${Y:-a b}"`, want: []string{"${X:-def}", "${Y:-a b}"}},
		{name: "braces", in: "echo {a,b} {1..3}", want: []string{"echo", "{a,b}", "{1..3}"}},
		{name: "command substitution", in: "$(id) `id`", want: []string{"$(id)", "`id`"}},
		{name: "arithmetic", in: "$((1+2))", want: []string{"$((1+2))"}},
		{name: "globs and tilde", in: "*.txt ~ ~/x", want: []string{"*.txt", "~", "~/x"}},
		{name: "mixed word", in: `pre$X"mid $Y"'$Z'`, want: []string{"pre$Xmid $Y$Z"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Split(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitErrors(t *testing.T) {
	for _, in := range []string{`"unterminated`, "a; b", "a | b"} {
		t.Run(in, func(t *testing.T) {
			_, err := Split(in)
			assert.Error(t, err)
		})
	}
}
