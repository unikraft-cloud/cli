// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shellbuiltins

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/plugins/sandbox"

	"unikraft.com/x/shell/builtins"
	"unikraft.com/x/stdio"

	"unikraft.com/cli/internal/config"
)

func newTestBuiltins(t *testing.T) *builtins.Kong {
	t.Helper()

	answers, err := newShellBuiltins(shellBuiltins{key: "inst-1"})
	require.NoError(t, err)
	return answers
}

// runBuiltin runs the builtin the line names, as the session would.
func runBuiltin(t *testing.T, answers *builtins.Kong, streams stdio.Stdio, line ...string) (int, error) {
	t.Helper()

	b, ok := answers.Builtins()[line[0]]
	require.True(t, ok, "no builtin %q", line[0])
	return b.Run(t.Context(), streams, line)
}

func TestShellBuiltinNames(t *testing.T) {
	answers, err := New("inst-1", sandbox.Target{})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"edit", "get", "help", "mount", "restart", "start", "stop", "suspend",
		"unmount", "volumes",
	}, slices.Sorted(maps.Keys(answers)),
		"every command of the grammar answers, by the name the session routes on")
}

func TestWithCredentialsIsTheProfileTheBuiltinsRunWith(t *testing.T) {
	ctx := WithCredentials(t.Context(), Credentials{Token: "t0ken", Metro: "fra0"})

	profile, err := config.G(ctx).CurrentProfile()
	require.NoError(t, err)
	assert.Equal(t, "t0ken", profile.Token)
	require.Len(t, profile.Metros, 1)
	assert.Equal(t, "fra0", profile.Metros[0].Name)
	assert.Equal(t, "https://api.fra0.unikraft.cloud", profile.Metros[0].Endpoint)
	require.NotNil(t, profile.Metros[0].Insecure)
	assert.False(t, *profile.Metros[0].Insecure)

	ctx = WithCredentials(t.Context(), Credentials{Token: "t0ken", Metro: "https://10.0.0.7:8443", Insecure: true})
	profile, err = config.G(ctx).CurrentProfile()
	require.NoError(t, err)
	require.Len(t, profile.Metros, 1)
	assert.Equal(t, "https://10.0.0.7:8443", profile.Metros[0].Endpoint)
	require.NotNil(t, profile.Metros[0].Insecure)
	assert.True(t, *profile.Metros[0].Insecure)
}

func TestShellBuiltinHelp(t *testing.T) {
	answers := newTestBuiltins(t)

	var out bytes.Buffer
	code, err := runBuiltin(t, answers, stdio.Stdio{Stdout: &out}, "help")

	require.NoError(t, err)
	assert.Zero(t, code)

	printed := out.String()
	assert.Contains(t, printed, ":mount <volume> <path>")
	assert.Contains(t, printed, ":unmount <volume>")
	assert.Contains(t, printed, ":edit <field=value>")
	assert.Contains(t, printed, "Detach a volume from this instance.")
	for name := range answers.Builtins() {
		assert.Contains(t, printed, ":"+name)
	}
}
