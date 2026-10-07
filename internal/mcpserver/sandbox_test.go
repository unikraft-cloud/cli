// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcpserver"
)

type fakeSandbox struct {
	calls []string
	data  []byte
}

func (f *fakeSandbox) Exec(ctx context.Context, stdio config.Stdio, instance string, opts mcpserver.SandboxExecOptions) (int, error) {
	f.calls = append(f.calls, fmt.Sprintf("exec %s args=%v cmdline=%q dir=%q env=%v stdin=%q", instance, opts.Args, opts.Cmdline, opts.Dir, opts.Env, opts.Stdin))
	switch opts.Cmdline {
	case "sleep":
		<-ctx.Done()
		return 0, ctx.Err()
	case "boom":
		return 0, errors.New("transport broke")
	}
	fmt.Fprint(stdio.Stdout, "out\n")
	if opts.Cmdline == "false" {
		return 1, nil
	}
	return 0, nil
}

func (f *fakeSandbox) ReadFile(_ context.Context, instance, path string) ([]byte, error) {
	f.calls = append(f.calls, "read "+instance+" "+path)
	return f.data, nil
}

func (f *fakeSandbox) WriteFile(_ context.Context, instance, path string, data []byte, opts mcpserver.SandboxWriteOptions) error {
	f.calls = append(f.calls, fmt.Sprintf("write %s %s %q append=%t parents=%t", instance, path, data, opts.Append, opts.Parents))
	return nil
}

func TestSandboxTools(t *testing.T) {
	ctx := t.Context()
	fake := &fakeSandbox{data: []byte("hello\n")}
	srv, err := mcpserver.New(ctx, mcpserver.Options{})
	require.NoError(t, err)
	require.NoError(t, srv.AddSandboxTools(fake))
	session := connect(t, ctx, srv)

	tools := listTools(t, ctx, session)
	assert.ElementsMatch(t, []string{"sandbox_exec", "sandbox_read", "sandbox_write"}, slices.Collect(maps.Keys(tools)))
	assert.True(t, tools["sandbox_read"].Annotations.ReadOnlyHint)
	assert.False(t, tools["sandbox_exec"].Annotations.ReadOnlyHint)
	assert.False(t, tools["sandbox_write"].Annotations.ReadOnlyHint)
	assert.Equal(t, "Run in sandbox", tools["sandbox_exec"].Title)
	exec := tools["sandbox_exec"].InputSchema.(map[string]any)
	assert.ElementsMatch(t, []any{"key", "command"}, exec["required"])
	assert.Len(t, properties(t, exec)["command"].(map[string]any)["anyOf"], 2)
	assert.Equal(t, "integer", properties(t, exec)["timeout_ms"].(map[string]any)["type"])
	assert.ElementsMatch(t, []any{"key", "path", "content"}, tools["sandbox_write"].InputSchema.(map[string]any)["required"])

	res := call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": []string{"ls", "/data"}, "dir": "/", "env": map[string]string{"A": "1"}, "stdin": "in"})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, "out\n", text(res))
	assert.Nil(t, res.StructuredContent)

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": "false"})
	assert.True(t, res.IsError)
	assert.Equal(t, "out\n\nexit status 1", text(res))

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": "boom"})
	assert.True(t, res.IsError)
	assert.Contains(t, text(res), "transport broke")

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": "sleep", "timeout_ms": 20})
	assert.True(t, res.IsError)
	assert.Contains(t, text(res), "command timed out after 20ms")

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": 7})
	assert.True(t, res.IsError)
	assert.Contains(t, text(res), "invalid command")

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": []string{}})
	assert.True(t, res.IsError)
	assert.Contains(t, text(res), "invalid command: it is empty")

	res = call(t, ctx, session, "sandbox_read", map[string]any{"key": "scratch", "path": "/data/x"})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, "hello\n", text(res))

	fake.data = []byte{0xff, 0xfe, 0x00}
	res = call(t, ctx, session, "sandbox_read", map[string]any{"key": "scratch", "path": "/data/bin"})
	assert.True(t, res.IsError)
	assert.Contains(t, text(res), "/data/bin is not UTF-8 text")

	res = call(t, ctx, session, "sandbox_write", map[string]any{"key": "scratch", "path": "/data/y", "content": "hi\n", "append": true, "parents": true})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, "3 bytes appended to /data/y\n", text(res))

	res = call(t, ctx, session, "sandbox_write", map[string]any{"key": "scratch", "path": "/data/z", "content": ""})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, "0 bytes written to /data/z\n", text(res))

	assert.Equal(t, []string{
		`exec scratch args=[ls /data] cmdline="" dir="/" env=map[A:1] stdin="in"`,
		`exec scratch args=[] cmdline="false" dir="" env=map[] stdin=""`,
		`exec scratch args=[] cmdline="boom" dir="" env=map[] stdin=""`,
		`exec scratch args=[] cmdline="sleep" dir="" env=map[] stdin=""`,
		"read scratch /data/x",
		"read scratch /data/bin",
		`write scratch /data/y "hi\n" append=true parents=true`,
		`write scratch /data/z "" append=false parents=false`,
	}, fake.calls)
}

func TestSandboxToolsReadOnly(t *testing.T) {
	ctx := t.Context()
	srv, err := mcpserver.New(ctx, mcpserver.Options{ReadOnly: true})
	require.NoError(t, err)
	require.NoError(t, srv.AddSandboxTools(&fakeSandbox{}))
	assert.Equal(t, []string{"sandbox_read"}, slices.Collect(maps.Keys(listTools(t, ctx, connect(t, ctx, srv)))))
}

func TestSandboxToolsResources(t *testing.T) {
	ctx := t.Context()
	srv, err := mcpserver.New(ctx, mcpserver.Options{Resources: []string{"instance"}})
	require.NoError(t, err)
	require.NoError(t, srv.AddSandboxTools(&fakeSandbox{}))
	assert.Empty(t, listTools(t, ctx, connect(t, ctx, srv)))
	assert.EqualError(t, srv.CheckResources(), "unknown resource types: instance; known: sandbox")
}
