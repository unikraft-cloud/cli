// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/mcpserver"
	resourcet "unikraft.com/cli/internal/resource/testing"
)

func newSession(t *testing.T, readOnly bool) (context.Context, *mcp.ClientSession, *resourcet.TestEnv) {
	t.Helper()
	env := resourcet.NewTestEnv()
	ctx := resourcet.WithTestEnv(t.Context(), env)

	srv, err := mcpserver.New(ctx, mcpserver.Options{ReadOnly: readOnly})
	require.NoError(t, err)
	require.NoError(t, srv.AddTools[resourcet.TestResource](ctx))
	return ctx, connect(t, ctx, srv), env
}

func connect(t *testing.T, ctx context.Context, srv *mcpserver.Server) *mcp.ClientSession {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	_, err := srv.Connect(ctx, serverTransport)
	require.NoError(t, err)

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func listTools(t *testing.T, ctx context.Context, session *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	res, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	tools := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	return tools
}

func call(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	return res
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func resources(t *testing.T, res *mcp.CallToolResult) []map[string]any {
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

func properties(t *testing.T, schema any) map[string]any {
	t.Helper()
	object, ok := schema.(map[string]any)
	require.True(t, ok, "schema %T is not an object", schema)
	props, _ := object["properties"].(map[string]any)
	return props
}

func TestListTools(t *testing.T) {
	ctx, session, _ := newSession(t, false)
	tools := listTools(t, ctx, session)

	assert.ElementsMatch(t,
		[]string{"test_list", "test_create", "test_edit", "test_delete"},
		slices.Collect(maps.Keys(tools)),
	)

	assert.True(t, tools["test_list"].Annotations.ReadOnlyHint)
	assert.False(t, tools["test_create"].Annotations.ReadOnlyHint)
	assert.False(t, *tools["test_edit"].Annotations.DestructiveHint)
	assert.True(t, *tools["test_delete"].Annotations.DestructiveHint)

	assert.Contains(t, tools["test_list"].Description, "settings.foo")

	create := properties(t, tools["test_create"].InputSchema)
	set := properties(t, create["set"])
	assert.ElementsMatch(t, []string{"name", "create_only", "settings.foo", "settings.bar"}, slices.Collect(maps.Keys(set)))
	foo := set["settings.foo"].(map[string]any)
	assert.Equal(t, []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}, foo["anyOf"])
	assert.Equal(t, "Foo setting.", foo["description"])

	edit := tools["test_edit"].InputSchema.(map[string]any)
	assert.Equal(t, []any{"key"}, edit["required"])
	editSet := properties(t, properties(t, edit)["set"])
	assert.ElementsMatch(t, []string{"edit_only", "settings.foo", "settings.bar"}, slices.Collect(maps.Keys(editSet)))

	assert.Equal(t, []any{"keys"}, tools["test_delete"].InputSchema.(map[string]any)["required"])
}

func TestReadOnly(t *testing.T) {
	ctx, session, _ := newSession(t, true)
	assert.Equal(t, []string{"test_list"}, slices.Collect(maps.Keys(listTools(t, ctx, session))))
}

func TestCallList(t *testing.T) {
	ctx, session, env := newSession(t, false)
	env.Add(resourcet.TestResource{ID: "1", Name: "a", State: "running"})
	env.Add(resourcet.TestResource{ID: "2", Name: "b", State: "stopped"})

	res := call(t, ctx, session, "test_list", map[string]any{
		"filter": []string{"state==running"},
		"fields": []string{"name"},
	})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, []map[string]any{{"name": "a"}}, resources(t, res))
	assert.Contains(t, text(res), `"name": "a"`)

	res = call(t, ctx, session, "test_list", map[string]any{"sort": []string{"-name"}, "fields": []string{"name"}})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, []map[string]any{{"name": "b"}, {"name": "a"}}, resources(t, res))
}

func TestCallListKeys(t *testing.T) {
	ctx, session, env := newSession(t, false)
	env.Add(resourcet.TestResource{ID: "1", Name: "a"})
	env.Add(resourcet.TestResource{ID: "2", Name: "b"})

	res := call(t, ctx, session, "test_list", map[string]any{"keys": []string{"b"}, "fields": []string{"id", "name"}})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, []map[string]any{{"id": "2", "name": "b"}}, resources(t, res))

	res = call(t, ctx, session, "test_list", map[string]any{"keys": []string{"missing"}})
	require.False(t, res.IsError, text(res))
	assert.Empty(t, resources(t, res))
}

func TestCallCreate(t *testing.T) {
	ctx, session, env := newSession(t, false)

	res := call(t, ctx, session, "test_create", map[string]any{
		"set": map[string]any{"name": "fresh", "settings.foo": 7, "settings.bar": "baz"},
	})
	require.False(t, res.IsError, text(res))
	require.Contains(t, env.Store, "fresh")
	assert.Equal(t, 7, env.Store["fresh"].Settings.Foo)
	assert.Equal(t, "baz", env.Store["fresh"].Settings.Bar)
	created := resources(t, res)
	require.Len(t, created, 1)
	assert.Equal(t, "fresh", created[0]["name"])

	res = call(t, ctx, session, "test_create", map[string]any{"set": map[string]any{"nope": "x"}})
	assert.True(t, res.IsError)
	assert.Contains(t, text(res), "nope")

	res = call(t, ctx, session, "test_create", map[string]any{
		"set":     map[string]any{"name": "preview"},
		"dry_run": true,
	})
	require.False(t, res.IsError, text(res))
	assert.Nil(t, res.StructuredContent)
	assert.Contains(t, text(res), "preview")
	assert.NotContains(t, env.Store, "preview")
}

func TestCallEdit(t *testing.T) {
	ctx, session, env := newSession(t, false)
	env.Add(resourcet.TestResource{ID: "1", Name: "a", Settings: resourcet.TestSettings{Foo: 1, Bar: "old"}})

	res := call(t, ctx, session, "test_edit", map[string]any{
		"key": "a",
		"set": map[string]any{"settings.bar": "new", "settings.foo": "5"},
	})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, "new", env.Store["a"].Settings.Bar)
	assert.Equal(t, 5, env.Store["a"].Settings.Foo)
	updated := resources(t, res)
	require.Len(t, updated, 1)
	assert.Equal(t, "new", updated[0]["settings"].(map[string]any)["bar"])

	res = call(t, ctx, session, "test_edit", map[string]any{
		"key":     "a",
		"set":     map[string]any{"settings.bar": "later"},
		"dry_run": true,
	})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, "new", env.Store["a"].Settings.Bar)
	assert.Contains(t, text(res), "later")

	res = call(t, ctx, session, "test_edit", map[string]any{"key": "missing", "set": map[string]any{"settings.bar": "x"}})
	assert.True(t, res.IsError)
	assert.Contains(t, text(res), "not found")
}

func TestCallDelete(t *testing.T) {
	ctx, session, env := newSession(t, false)
	env.Add(resourcet.TestResource{ID: "1", Name: "a"})
	env.Add(resourcet.TestResource{ID: "2", Name: "b"})

	res := call(t, ctx, session, "test_delete", map[string]any{"keys": []string{"a"}})
	require.False(t, res.IsError, text(res))
	assert.NotContains(t, env.Store, "a")
	assert.Contains(t, env.Store, "b")
	deleted := resources(t, res)
	require.Len(t, deleted, 1)
	assert.Equal(t, "a", deleted[0]["name"])

	res = call(t, ctx, session, "test_delete", map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, text(res), "no tests specified")
}

func TestCallLogging(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.TraceLevel)
	env := resourcet.NewTestEnv()
	ctx := log.WithLogger(resourcet.WithTestEnv(t.Context(), env), &logger)
	srv, err := mcpserver.New(ctx, mcpserver.Options{})
	require.NoError(t, err)
	require.NoError(t, srv.AddTools[resourcet.TestResource](ctx))
	session := connect(t, ctx, srv)

	call(t, ctx, session, "test_list", map[string]any{"fields": []string{"name"}})
	call(t, ctx, session, "test_create", map[string]any{"set": map[string]any{"nope": "x"}})
	call(t, ctx, session, "test_delete", map[string]any{})

	out := logs.String()
	assert.Contains(t, out, `"level":"trace","tool":"test_list","arguments":{"fields":["name"]},"message":"calling tool"`)
	assert.Contains(t, out, `"level":"debug","tool":"test_list","took":`)
	assert.Contains(t, out, `"message":"tool call complete"`)
	assert.Contains(t, out, `"tool":"test_create","took":`)
	assert.Contains(t, out, `"error":"unknown fields: [nope]","message":"tool call failed"`)
	assert.Contains(t, out, `"tool":"test_delete","error":"no tests specified","message":"tool call rejected"`)
}
