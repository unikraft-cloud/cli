// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcpserver/mcptest"
)

func TestMCPServerTools(t *testing.T) {
	ctx := config.WithConfig(t.Context(), &config.Config{})

	readOnlyTools := []string{
		"instance_list", "instance_template_list", "instance_checkpoint_list",
		"volume_list", "volume_template_list", "service_list", "certificate_list",
		"image_list", "image_get", "metro_list", "profile_list", "instance_logs", "sandbox_read",
	}
	mutatingTools := []string{
		"instance_create", "instance_edit", "instance_delete", "instance_control",
		"instance_template_create", "instance_template_edit", "instance_template_delete",
		"instance_checkpoint_create", "instance_checkpoint_edit", "instance_checkpoint_delete",
		"volume_create", "volume_edit", "volume_delete",
		"volume_template_create", "volume_template_edit", "volume_template_delete",
		"service_create", "service_edit", "service_delete",
		"certificate_create", "certificate_delete", "image_delete",
		"sandbox_exec", "sandbox_write",
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
