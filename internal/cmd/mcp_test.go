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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
)

func TestMCPServerTools(t *testing.T) {
	ctx := config.WithConfig(t.Context(), &config.Config{})

	readOnlyTools := []string{
		"instance_list", "instance_template_list", "instance_checkpoint_list",
		"volume_list", "volume_template_list", "service_list", "certificate_list",
		"image_list", "image_get", "metro_list", "profile_list", "instance_logs",
	}
	mutatingTools := []string{
		"instance_create", "instance_edit", "instance_delete", "instance_template_create",
		"volume_template_delete", "service_edit", "certificate_create", "certificate_delete", "image_delete",
		"instance_control",
	}
	mutatingSuffixes := []string{"_create", "_edit", "_delete", "_control"}

	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("read-only=%t", readOnly), func(t *testing.T) {
			srv, err := newMCPServer(ctx, io.Discard, nil, MCPCmd{ReadOnly: readOnly})
			require.NoError(t, err)

			clientTransport, serverTransport := mcp.NewInMemoryTransports()
			_, err = srv.Connect(ctx, serverTransport)
			require.NoError(t, err)
			session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = session.Close() })

			res, err := session.ListTools(ctx, nil)
			require.NoError(t, err)
			tools := make(map[string]*mcp.Tool, len(res.Tools))
			names := make([]string, 0, len(res.Tools))
			for _, tool := range res.Tools {
				tools[tool.Name] = tool
				names = append(names, tool.Name)
			}

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

			control := tools["instance_control"].InputSchema.(map[string]any)
			assert.ElementsMatch(t, []any{"action", "instances"}, control["required"])
			action := control["properties"].(map[string]any)["action"].(map[string]any)
			assert.Equal(t, []any{"start", "stop", "restart", "suspend"}, action["enum"])
		})
	}
}
