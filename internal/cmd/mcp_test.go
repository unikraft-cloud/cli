// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/pkg/mcpserver/mcptest"
)

func TestMCPServerTools(t *testing.T) {
	ctx := config.WithConfig(t.Context(), &config.Config{})

	readOnlyTools := []string{
		"instance_list", "instance_template_list", "instance_checkpoint_list",
		"volume_list", "volume_template_list", "service_list", "certificate_list",
		"image_list", "image_get", "metro_list", "instance_logs", "sandbox_read",
	}
	mutatingTools := []string{
		"instance_create", "instance_edit", "instance_delete", "instance_control",
		"instance_template_create", "instance_template_edit", "instance_template_delete",
		"instance_checkpoint_create", "instance_checkpoint_edit", "instance_checkpoint_delete",
		"volume_create", "volume_edit", "volume_delete",
		"volume_template_create", "volume_template_edit", "volume_template_delete",
		"service_create", "service_edit", "service_delete",
		"certificate_create", "certificate_edit", "certificate_delete", "image_delete",
		"sandbox_exec", "sandbox_write", "profile_use",
	}

	srv, err := newMCPServer(ctx, io.Discard, nil, MCPCmd{})
	require.NoError(t, err)
	assert.ElementsMatch(t, append(readOnlyTools, mutatingTools...), mcptest.ConnectServer(t, ctx, srv.MCP()).ToolNames())

	srv, err = newMCPServer(ctx, io.Discard, nil, MCPCmd{ReadOnly: true})
	require.NoError(t, err)
	assert.ElementsMatch(t, readOnlyTools, mcptest.ConnectServer(t, ctx, srv.MCP()).ToolNames())
}

func TestMCPServerResources(t *testing.T) {
	ctx := config.WithConfig(t.Context(), &config.Config{})

	srv, err := newMCPServer(ctx, io.Discard, nil, MCPCmd{Resources: []string{"instance", "metro"}})
	require.NoError(t, err)
	session := mcptest.ConnectServer(t, ctx, srv.MCP())
	assert.ElementsMatch(t, []string{
		"instance_list", "instance_create", "instance_edit", "instance_delete", "instance_control", "instance_logs", "metro_list",
	}, session.ToolNames())
	assert.Contains(t, session.Instructions, "Only the tools of these resource types are served: instance, metro.")

	_, err = newMCPServer(ctx, io.Discard, nil, MCPCmd{Resources: []string{"instance", "nope"}})
	assert.EqualError(t, err, "--resources: unknown resource types: nope; known: certificate, image, instance, instance_checkpoint, instance_template, metro, profile, sandbox, service, volume, volume_template")
}

func TestMCPResourceTypes(t *testing.T) {
	ctx := config.WithConfig(t.Context(), &config.Config{})

	srv, err := newMCPServer(ctx, io.Discard, nil, MCPCmd{})
	require.NoError(t, err)
	for _, tool := range mcptest.ConnectServer(t, ctx, srv.MCP()).ToolNames() {
		assert.Truef(t, slices.ContainsFunc(mcpResourceTypes, func(typ string) bool {
			return strings.HasPrefix(tool, typ+"_")
		}), "tool %s belongs to no type in mcpResourceTypes", tool)
	}

	for _, typ := range mcpResourceTypes {
		srv, err := newMCPServer(ctx, io.Discard, nil, MCPCmd{Resources: []string{typ}})
		require.NoError(t, err)
		assert.NotEmptyf(t, mcptest.ConnectServer(t, ctx, srv.MCP()).ToolNames(), "type %s serves no tools", typ)
	}
}

func TestMCPProfileUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig := func(profile string) {
		require.NoError(t, os.WriteFile(path, []byte("profile: "+profile+`
profiles:
  a: {type: cloud, token: a, metros: [{name: fra, endpoint: https://api.fra.unikraft.cloud}]}
  b: {type: cloud, token: b, metros: [{name: was, endpoint: https://api.was.unikraft.cloud}]}
`), 0o600))
	}
	writeConfig("a")
	cfg, err := loadConfig(path, "")
	require.NoError(t, err)
	ctx := config.WithConfig(t.Context(), cfg)
	srv, err := newMCPServer(ctx, io.Discard, nil, MCPCmd{})
	require.NoError(t, err)
	session := mcptest.ConnectServer(t, ctx, srv.MCP())

	active := func(args map[string]any) string {
		res := mcptest.CallTool(t, ctx, session, "profile_use", args)
		require.False(t, res.IsError, mcptest.Text(res))
		var names []string
		for _, profile := range mcptest.Resources(t, res) {
			if profile["active"] == true {
				names = append(names, profile["name"].(string))
			}
		}
		require.Len(t, names, 1)
		return names[0]
	}

	writeConfig("b")
	assert.Equal(t, "b", active(map[string]any{}))
	assert.Equal(t, "a", active(map[string]any{"name": "a"}))
	assert.Equal(t, "a", active(map[string]any{}))

	require.NoError(t, os.WriteFile(path, []byte("profiles: ["), 0o600))
	res := mcptest.CallTool(t, ctx, session, "profile_use", map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "loading config")

	writeConfig("a")
	cfg, err = loadConfig(path, "b")
	require.NoError(t, err)
	pinned := config.WithConfig(t.Context(), cfg)
	srv, err = newMCPServer(pinned, io.Discard, nil, MCPCmd{})
	require.NoError(t, err)
	session = mcptest.ConnectServer(t, pinned, srv.MCP())
	assert.NotContains(t, session.ToolNames(), "profile_use")
	res = mcptest.CallTool(t, pinned, session, "metro_list", map[string]any{"fields": []string{"name"}})
	assert.Equal(t, []map[string]any{{"name": "was"}}, mcptest.Resources(t, res))

	t.Setenv("UNIKRAFT_PROFILE_ENV", "1")
	srv, err = newMCPServer(ctx, io.Discard, nil, MCPCmd{})
	require.NoError(t, err)
	assert.NotContains(t, mcptest.ConnectServer(t, ctx, srv.MCP()).ToolNames(), "profile_use")
}
