// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcpserver"
	"unikraft.com/cli/internal/mcpserver/mcptest"
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

func sandboxTools(commands mcpserver.SandboxCommands) func(*mcpserver.Server) error {
	return func(s *mcpserver.Server) error { return s.AddSandboxTools(commands) }
}

func TestSandboxTools(t *testing.T) {
	ctx := t.Context()
	fake := &fakeSandbox{data: []byte("hello\n")}
	session := mcptest.ConnectServer(t, ctx, newServer(t, ctx, mcpserver.Options{}, sandboxTools(fake)))

	assert.ElementsMatch(t, []string{"sandbox_exec", "sandbox_read", "sandbox_write"}, session.ToolNames())
	assert.True(t, session.Tool("sandbox_read").Annotations.ReadOnlyHint)
	assert.False(t, session.Tool("sandbox_write").Annotations.ReadOnlyHint)
	exec := session.Tool("sandbox_exec")
	assert.False(t, exec.Annotations.ReadOnlyHint)
	assert.Equal(t, "Run in sandbox", exec.Title)
	assert.ElementsMatch(t, []any{"key", "command"}, exec.InputSchema.(map[string]any)["required"])
	assert.Len(t, property(t, exec.InputSchema, "command")["anyOf"], 2)
	assert.Equal(t, "integer", property(t, exec.InputSchema, "timeout_ms")["type"])
	assert.ElementsMatch(t, []any{"key", "path", "content"}, session.Tool("sandbox_write").InputSchema.(map[string]any)["required"])

	res := call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": []string{"ls", "/data"}, "dir": "/", "env": map[string]string{"A": "1"}, "stdin": "in"})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "out\n", mcptest.Text(res))
	assert.Nil(t, res.StructuredContent)

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": "false"})
	assert.True(t, res.IsError)
	assert.Equal(t, "out\n\nexit status 1", mcptest.Text(res))

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": "boom"})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "transport broke")

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": "sleep", "timeout_ms": 20})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "command timed out after 20ms")

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": 7})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "invalid command")

	res = call(t, ctx, session, "sandbox_exec", map[string]any{"key": "scratch", "command": []string{}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "invalid command: it is empty")

	res = call(t, ctx, session, "sandbox_read", map[string]any{"key": "scratch", "path": "/data/x"})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "hello\n", mcptest.Text(res))

	fake.data = []byte{0xff, 0xfe, 0x00}
	res = call(t, ctx, session, "sandbox_read", map[string]any{"key": "scratch", "path": "/data/bin"})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "/data/bin is not UTF-8 text")

	res = call(t, ctx, session, "sandbox_write", map[string]any{"key": "scratch", "path": "/data/y", "content": "hi\n", "append": true, "parents": true})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "3 bytes appended to /data/y\n", mcptest.Text(res))

	res = call(t, ctx, session, "sandbox_write", map[string]any{"key": "scratch", "path": "/data/z", "content": ""})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "0 bytes written to /data/z\n", mcptest.Text(res))

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

func TestSandboxToolsGating(t *testing.T) {
	ctx := t.Context()
	srv := newServer(t, ctx, mcpserver.Options{ReadOnly: true}, sandboxTools(&fakeSandbox{}))
	assert.Equal(t, []string{"sandbox_read"}, mcptest.ConnectServer(t, ctx, srv).ToolNames())

	srv = newServer(t, ctx, mcpserver.Options{Resources: []string{"instance"}}, sandboxTools(&fakeSandbox{}))
	assert.Empty(t, mcptest.ConnectServer(t, ctx, srv).Tools)
	assert.EqualError(t, srv.CheckResources(), "unknown resource types: instance; known: sandbox")
}
