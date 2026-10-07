// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"fmt"
	"io"
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
		"instance_create", "instance_edit", "instance_delete", "instance_template_create",
		"volume_template_delete", "service_edit", "certificate_create", "certificate_delete", "image_delete",
		"instance_control", "sandbox_exec", "sandbox_write",
	}
	mutatingSuffixes := []string{"_create", "_edit", "_delete", "_control", "_exec", "_write"}

	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("read-only=%t", readOnly), func(t *testing.T) {
			srv, err := newMCPServer(ctx, io.Discard, nil, MCPCmd{ReadOnly: readOnly})
			require.NoError(t, err)
			names := mcptest.ConnectServer(t, ctx, srv).ToolNames()

			assert.Subset(t, names, readOnlyTools)
			assert.NotContains(t, names, "instance_get")
			assert.NotContains(t, names, "metro_get")
			assert.NotContains(t, names, "certificate_edit")
			assert.NotContains(t, names, "metro_create")
			assert.NotContains(t, names, "instance_start")

			if readOnly {
				for _, name := range names {
					for _, suffix := range mutatingSuffixes {
						assert.False(t, strings.HasSuffix(name, suffix), "read-only server exposes %s", name)
					}
				}
				return
			}
			assert.Subset(t, names, mutatingTools)
		})
	}
}

func TestMCPServerResources(t *testing.T) {
	ctx := config.WithConfig(t.Context(), &config.Config{})

	srv, err := newMCPServer(ctx, io.Discard, nil, MCPCmd{Resources: []string{"instance", "metro"}})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"instance_list", "instance_create", "instance_edit", "instance_delete", "instance_control", "instance_logs", "metro_list",
	}, mcptest.ConnectServer(t, ctx, srv).ToolNames())

	_, err = newMCPServer(ctx, io.Discard, nil, MCPCmd{Resources: []string{"instance", "nope"}})
	assert.EqualError(t, err, "--resources: unknown resource types: nope; known: certificate, image, instance, instance_checkpoint, instance_template, metro, profile, sandbox, service, volume, volume_template")
}
