// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcptest

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Session is a client session with an MCP server, with the instructions and
// tools the server announced.
type Session struct {
	*mcp.ClientSession

	Instructions string
	Tools        []*mcp.Tool
}

// Connect opens a session over transport and closes it when the test ends.
func Connect(t testing.TB, transport mcp.Transport) *Session {
	t.Helper()
	ctx := t.Context()

	client := mcp.NewClient(&mcp.Implementation{Name: "unikraft-mcptest", Version: "0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.ListTools(ctx, nil)
	require.NoError(t, err)

	return &Session{
		ClientSession: session,
		Instructions:  session.InitializeResult().Instructions,
		Tools:         res.Tools,
	}
}

func ConnectServer(t testing.TB, ctx context.Context, srv *mcp.Server) *Session {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	_, err := srv.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	return Connect(t, clientTransport)
}

func (s *Session) Tool(name string) *mcp.Tool {
	i := slices.IndexFunc(s.Tools, func(tool *mcp.Tool) bool { return tool.Name == name })
	if i < 0 {
		return nil
	}
	return s.Tools[i]
}

func (s *Session) ToolNames() []string {
	names := make([]string, 0, len(s.Tools))
	for _, tool := range s.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// Call runs the tool and gives its text output and whether the tool reported
// an error. A protocol failure is returned as the error.
func (s *Session) Call(ctx context.Context, name string, args json.RawMessage) (string, bool, error) {
	params := &mcp.CallToolParams{Name: name}
	if len(args) > 0 {
		params.Arguments = args
	}
	res, err := s.CallTool(ctx, params)
	if err != nil {
		return "", false, err
	}
	return Text(res), res.IsError, nil
}

func Text(res *mcp.CallToolResult) string {
	var parts []string
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func CallTool(t testing.TB, ctx context.Context, session *Session, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	return res
}

func Resources(t testing.TB, res *mcp.CallToolResult) []map[string]any {
	t.Helper()
	structured, ok := res.StructuredContent.(map[string]any)
	require.True(t, ok, "structured content %T is not an object", res.StructuredContent)
	items, ok := structured["resources"].([]any)
	require.True(t, ok, "resources %T is not a list", structured["resources"])
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.(map[string]any))
	}
	return out
}

func Properties(t testing.TB, schema any) map[string]any {
	t.Helper()
	object, ok := schema.(map[string]any)
	require.True(t, ok, "schema %T is not an object", schema)
	props, _ := object["properties"].(map[string]any)
	return props
}

func Property(t testing.TB, schema any, name string) map[string]any {
	t.Helper()
	prop, ok := Properties(t, schema)[name].(map[string]any)
	require.True(t, ok, "property %s is not an object", name)
	return prop
}
