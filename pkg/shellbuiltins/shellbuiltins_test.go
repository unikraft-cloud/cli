// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shellbuiltins

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/plugins/sandbox"

	"unikraft.com/x/shell/builtins"
	"unikraft.com/x/stdio"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/pkg/types"
)

type fakeInstance struct {
	calls []any
}

type (
	getCall    builtins.Format
	editCall   map[string]string
	attachCall struct {
		volume, at string
		readonly   bool
	}
	detachCall  string
	stopCall    StopOpts
	restartCall StopOpts
	suspendCall types.DurationMS
)

func (f *fakeInstance) Get(_ context.Context, _ stdio.Stdio, format builtins.Format) error {
	f.calls = append(f.calls, getCall(format))
	return nil
}

func (f *fakeInstance) Volumes(context.Context, stdio.Stdio, builtins.Format) error { return nil }
func (f *fakeInstance) Start(context.Context, stdio.Stdio) error                    { return nil }

func (f *fakeInstance) Edit(_ context.Context, _ stdio.Stdio, set map[string]string) error {
	f.calls = append(f.calls, editCall(set))
	return nil
}

func (f *fakeInstance) Attach(_ context.Context, _ stdio.Stdio, volume, at string, readonly bool) error {
	f.calls = append(f.calls, attachCall{volume, at, readonly})
	return nil
}

func (f *fakeInstance) Detach(_ context.Context, _ stdio.Stdio, volume string) error {
	f.calls = append(f.calls, detachCall(volume))
	return nil
}

func (f *fakeInstance) Stop(_ context.Context, _ stdio.Stdio, opts StopOpts) error {
	f.calls = append(f.calls, stopCall(opts))
	return nil
}

func (f *fakeInstance) Restart(_ context.Context, _ stdio.Stdio, opts StopOpts) error {
	f.calls = append(f.calls, restartCall(opts))
	return nil
}

func (f *fakeInstance) Suspend(_ context.Context, _ stdio.Stdio, drainTimeout types.DurationMS) error {
	f.calls = append(f.calls, suspendCall(drainTimeout))
	return nil
}

func newTestBuiltins(t *testing.T, inst Instance) *builtins.Kong {
	t.Helper()

	answers, err := newShellBuiltins(shellBuiltins{inst: inst})
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
	answers, err := New(&fakeInstance{}, sandbox.Target{})
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

	ctx = WithCredentials(t.Context(), Credentials{Token: "t0ken", Metro: "fra0", Endpoint: "https://node-3.fra0.unikraft.cloud"})
	profile, err = config.G(ctx).CurrentProfile()
	require.NoError(t, err)
	require.Len(t, profile.Metros, 1)
	assert.Equal(t, "fra0", profile.Metros[0].Name)
	assert.Equal(t, "https://node-3.fra0.unikraft.cloud", profile.Metros[0].Endpoint)
}

func TestShellBuiltinHelp(t *testing.T) {
	answers := newTestBuiltins(t, &fakeInstance{})

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

func TestShellBuiltinsCallInstance(t *testing.T) {
	for _, tc := range []struct {
		line []string
		want any
	}{
		{[]string{"get"}, getCall{}},
		{[]string{"get", "-o", "json", "-f", "name,state"}, getCall{Field: []string{"name", "state"}, Output: "json"}},
		{[]string{"edit", "memory=256Mi", "env=A=B"}, editCall{"memory": "256Mi", "env": "A=B"}},
		{[]string{"mount", "--readonly", "vol", "/data"}, attachCall{"vol", "/data", true}},
		{[]string{"unmount", "vol"}, detachCall("vol")},
		{[]string{"stop"}, stopCall{DrainTimeout: -1}},
		{[]string{"stop", "--force", "--drain-timeout=500"}, stopCall{Force: true, DrainTimeout: 500}},
		{[]string{"stop", "--drain-timeout=1s"}, stopCall{DrainTimeout: 1000}},
		{[]string{"restart", "--force"}, restartCall{Force: true, DrainTimeout: -1}},
		{[]string{"suspend", "--drain-timeout=100"}, suspendCall(100)},
	} {
		t.Run(strings.Join(tc.line, " "), func(t *testing.T) {
			inst := &fakeInstance{}
			code, err := runBuiltin(t, newTestBuiltins(t, inst), stdio.Stdio{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}, tc.line...)

			require.NoError(t, err)
			assert.Zero(t, code)
			assert.Equal(t, []any{tc.want}, inst.calls)
		})
	}
}
