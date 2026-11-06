// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
	"unikraft.com/cli/internal/mcpserver/mcptest"
)

func TestMCP(t *testing.T) {
	t.Run("sandbox", func(t *testing.T) {
		r := runner(t, true, []string{staging})
		image := integ.Busybox.Build(t, r)
		session := mcptest.Connect(t, &mcp.CommandTransport{
			Command: r.Command(context.WithoutCancel(t.Context()), []string{"unikraft", "mcp", "--docs-url="}),
		})
		call := func(tool string, args map[string]any) string {
			t.Helper()
			res := mcptest.CallTool(t, t.Context(), session, tool, args)
			require.False(t, res.IsError, mcptest.Text(res))
			return mcptest.Text(res)
		}

		name := "test-" + uniq()
		path := "/tmp/mcp.txt"
		call("instance_create", map[string]any{"set": map[string]any{
			"name":             name,
			"metro":            r.MetroName,
			"image":            image,
			"runtime.args":     "tail -f /dev/null",
			"plugins":          []string{"name=" + sandboxPlugin + ",rom=" + sandboxPluginRom},
			"resources.memory": "512MiB",
			"resources.vcpus":  1,
		}})
		r.Run(t, []string{"unikraft", "--timeout", "60s", "instance", "wait", "--until", "state==running", name}, integ.WithNoPartition())

		call("sandbox_write", map[string]any{"key": name, "path": path, "content": "hello from mcp"})
		assert.Contains(t, call("sandbox_exec", map[string]any{"key": name, "command": []string{"cat", path}}), "hello from mcp")
		assert.Equal(t, "hello from mcp", call("sandbox_read", map[string]any{"key": name, "path": path}))
		call("instance_delete", map[string]any{"keys": []string{name}})
	})
}
