// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcpserver"
	"unikraft.com/cli/internal/types"
)

type fakeInstances struct {
	calls []string
}

func (f *fakeInstances) Start(_ context.Context, stdio config.Stdio, instances []string) error {
	f.calls = append(f.calls, "start "+strings.Join(instances, ","))
	_, err := fmt.Fprintf(stdio.Stdout, `[{"name": %q, "state": "running"}]`, instances[0])
	return err
}

func (f *fakeInstances) Stop(_ context.Context, _ config.Stdio, instances []string, opts mcpserver.InstanceStopOptions) error {
	f.calls = append(f.calls, fmt.Sprintf("stop %s force=%t drain=%d", strings.Join(instances, ","), opts.Force, opts.DrainTimeout))
	return nil
}

func (f *fakeInstances) Restart(_ context.Context, _ config.Stdio, instances []string, opts mcpserver.InstanceStopOptions) error {
	f.calls = append(f.calls, fmt.Sprintf("restart %s force=%t drain=%d", strings.Join(instances, ","), opts.Force, opts.DrainTimeout))
	return nil
}

func (f *fakeInstances) Suspend(_ context.Context, _ config.Stdio, instances []string, drainTimeout types.DurationMS) error {
	f.calls = append(f.calls, fmt.Sprintf("suspend %s drain=%d", strings.Join(instances, ","), drainTimeout))
	return nil
}

func (f *fakeInstances) Logs(_ context.Context, stdio config.Stdio, instance string, tail int) error {
	f.calls = append(f.calls, fmt.Sprintf("logs %s tail=%d", instance, tail))
	fmt.Fprintln(stdio.Stdout, "line 1\nline 2")
	return nil
}

func TestInstanceTools(t *testing.T) {
	ctx := t.Context()
	fake := &fakeInstances{}
	srv, err := mcpserver.New(ctx, mcpserver.Options{})
	require.NoError(t, err)
	require.NoError(t, srv.AddInstanceTools(fake))
	session := connect(t, ctx, srv)

	tools := listTools(t, ctx, session)
	assert.ElementsMatch(t, []string{"instance_control", "instance_logs"}, slices.Collect(maps.Keys(tools)))
	assert.True(t, tools["instance_logs"].Annotations.ReadOnlyHint)
	assert.False(t, tools["instance_control"].Annotations.ReadOnlyHint)
	control := tools["instance_control"].InputSchema.(map[string]any)
	assert.ElementsMatch(t, []any{"action", "instances"}, control["required"])
	assert.Equal(t, []any{"start", "stop", "restart", "suspend"}, properties(t, control)["action"].(map[string]any)["enum"])
	assert.Equal(t, "array", properties(t, control)["instances"].(map[string]any)["type"])
	assert.Equal(t, "integer", properties(t, control)["drain_timeout_ms"].(map[string]any)["type"])

	res := call(t, ctx, session, "instance_control", map[string]any{"action": "start", "instances": []string{"web"}})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, []map[string]any{{"name": "web", "state": "running"}}, resources(t, res))

	for _, args := range []map[string]any{
		{"action": "stop", "instances": []string{"web", "db"}, "force": true},
		{"action": "restart", "instances": []string{"web"}, "drain_timeout_ms": 5000},
		{"action": "suspend", "instances": []string{"web"}},
	} {
		res = call(t, ctx, session, "instance_control", args)
		require.False(t, res.IsError, text(res))
	}

	res = call(t, ctx, session, "instance_control", map[string]any{"action": "explode", "instances": []string{"web"}})
	assert.True(t, res.IsError)
	assert.Contains(t, text(res), `unknown action "explode"`)

	res = call(t, ctx, session, "instance_logs", map[string]any{"instance": "web"})
	require.False(t, res.IsError, text(res))
	assert.Nil(t, res.StructuredContent)
	assert.Contains(t, text(res), "line 1")

	assert.Equal(t, []string{
		"start web",
		"stop web,db force=true drain=-1",
		"restart web force=false drain=5000",
		"suspend web drain=-1",
		"logs web tail=200",
	}, fake.calls)
}

func TestInstanceToolsReadOnly(t *testing.T) {
	ctx := t.Context()
	srv, err := mcpserver.New(ctx, mcpserver.Options{ReadOnly: true})
	require.NoError(t, err)
	require.NoError(t, srv.AddInstanceTools(&fakeInstances{}))

	tools := listTools(t, ctx, connect(t, ctx, srv))
	assert.Equal(t, []string{"instance_logs"}, slices.Collect(maps.Keys(tools)))
}
