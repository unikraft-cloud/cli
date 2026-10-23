// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcp_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcp"
	"unikraft.com/cli/internal/mcpserver/mcptest"
	modelmcp "unikraft.com/cli/internal/model/mcp"
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

func (f *fakeInstances) Stop(_ context.Context, _ config.Stdio, instances []string, opts mcp.InstanceStopOptions) error {
	f.calls = append(f.calls, fmt.Sprintf("stop %s force=%t drain=%d", strings.Join(instances, ","), opts.Force, opts.DrainTimeout))
	return nil
}

func (f *fakeInstances) Restart(_ context.Context, _ config.Stdio, instances []string, opts mcp.InstanceStopOptions) error {
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

func connect(t *testing.T, add func(*modelmcp.Tools) error) *mcptest.Session {
	t.Helper()
	srv := sdk.NewServer(&sdk.Implementation{Name: "test"}, nil)
	require.NoError(t, add(modelmcp.New(srv, modelmcp.Options{})))
	return mcptest.ConnectServer(t, t.Context(), srv)
}

func TestInstanceTools(t *testing.T) {
	ctx := t.Context()
	fake := &fakeInstances{}
	session := connect(t, func(tools *modelmcp.Tools) error { return mcp.AddInstanceTools(tools, fake) })

	assert.ElementsMatch(t, []string{"instance_control", "instance_logs"}, session.ToolNames())
	logs := session.Tool("instance_logs")
	assert.True(t, logs.Annotations.ReadOnlyHint)
	assert.True(t, strings.HasPrefix(logs.Description, "Read the last lines"), logs.Description)
	control := session.Tool("instance_control")
	assert.Equal(t, "Control instances", control.Title)
	assert.False(t, control.Annotations.ReadOnlyHint)
	assert.ElementsMatch(t, []any{"action", "keys"}, control.InputSchema.(map[string]any)["required"])
	assert.Equal(t, []any{"start", "stop", "restart", "suspend"}, mcptest.Property(t, control.InputSchema, "action")["enum"])
	assert.Equal(t, "array", mcptest.Property(t, control.InputSchema, "keys")["type"])
	assert.Equal(t, "integer", mcptest.Property(t, control.InputSchema, "drain_timeout_ms")["type"])

	res := mcptest.CallTool(t, ctx, session, "instance_control", map[string]any{"action": "start", "keys": []string{"web"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "web", "state": "running"}}, mcptest.Resources(t, res))

	for _, args := range []map[string]any{
		{"action": "stop", "keys": []string{"web", "db"}, "force": true},
		{"action": "restart", "keys": []string{"web"}, "drain_timeout_ms": 5000},
		{"action": "suspend", "keys": []string{"web"}},
	} {
		res = mcptest.CallTool(t, ctx, session, "instance_control", args)
		require.False(t, res.IsError, mcptest.Text(res))
	}

	res = mcptest.CallTool(t, ctx, session, "instance_control", map[string]any{"action": "explode", "keys": []string{"web"}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), `unknown action "explode"`)

	res = mcptest.CallTool(t, ctx, session, "instance_logs", map[string]any{"key": "web"})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Nil(t, res.StructuredContent)
	assert.Contains(t, mcptest.Text(res), "line 1")

	assert.Equal(t, []string{
		"start web",
		"stop web,db force=true drain=-1",
		"restart web force=false drain=5000",
		"suspend web drain=-1",
		"logs web tail=200",
	}, fake.calls)
}
