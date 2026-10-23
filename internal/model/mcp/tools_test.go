// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/mcpserver/mcptest"
	modelmcp "unikraft.com/cli/internal/model/mcp"
	modelt "unikraft.com/cli/internal/model/testing"
)

func newServer(t *testing.T, ctx context.Context, opts modelmcp.Options) *mcp.Server {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	require.NoError(t, modelmcp.New(srv, opts).AddTools[modelt.TestModel](ctx))
	return srv
}

func newSession(t *testing.T, opts modelmcp.Options) (context.Context, *mcptest.Session, *modelt.TestEnv) {
	t.Helper()
	env := modelt.NewTestEnv()
	ctx := modelt.WithTestEnv(t.Context(), env)
	return ctx, mcptest.ConnectServer(t, ctx, newServer(t, ctx, opts)), env
}

func TestResourcesAllowlist(t *testing.T) {
	ctx := modelt.WithTestEnv(t.Context(), modelt.NewTestEnv())

	srv := newServer(t, ctx, modelmcp.Options{Resources: []string{"Test"}})
	session := mcptest.ConnectServer(t, ctx, srv)
	assert.Len(t, session.Tools, 4)

	srv = newServer(t, ctx, modelmcp.Options{Resources: []string{"other-thing"}})
	assert.Empty(t, mcptest.ConnectServer(t, ctx, srv).Tools)
}

func TestListTools(t *testing.T) {
	_, session, _ := newSession(t, modelmcp.Options{})
	assert.ElementsMatch(t, []string{"test_list", "test_create", "test_edit", "test_delete"}, session.ToolNames())

	list := session.Tool("test_list")
	assert.True(t, list.Annotations.ReadOnlyHint)
	assert.Equal(t, "List tests", list.Title)
	assert.NotContains(t, list.Description, "settings.foo")
	filter := mcptest.Property(t, list.InputSchema, "filter")["description"]
	assert.Contains(t, filter, "settings.foo")
	assert.Contains(t, filter, "Field meanings: state: Lifecycle state: running or stopped.")

	create := session.Tool("test_create")
	assert.False(t, create.Annotations.ReadOnlyHint)
	assert.Equal(t, "Create test", create.Title)
	assert.True(t, strings.HasPrefix(create.Description, "Create a test. "), create.Description)
	assert.Contains(t, create.Description, "Required: none.")
	set := mcptest.Properties(t, mcptest.Property(t, create.InputSchema, "set"))
	assert.ElementsMatch(t, []string{"name", "create_only", "settings.foo", "settings.bar"}, slices.Collect(maps.Keys(set)))
	foo := set["settings.foo"].(map[string]any)
	assert.Equal(t, []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}, foo["anyOf"])
	assert.Equal(t, "Foo setting.", foo["description"])

	edit := session.Tool("test_edit")
	assert.False(t, *edit.Annotations.DestructiveHint)
	assert.Equal(t, []any{"key"}, edit.InputSchema.(map[string]any)["required"])
	editSet := mcptest.Properties(t, mcptest.Property(t, edit.InputSchema, "set"))
	assert.ElementsMatch(t, []string{"edit_only", "settings.foo", "settings.bar"}, slices.Collect(maps.Keys(editSet)))

	del := session.Tool("test_delete")
	assert.True(t, *del.Annotations.DestructiveHint)
	assert.Equal(t, []any{"keys"}, del.InputSchema.(map[string]any)["required"])
}

func TestReadOnly(t *testing.T) {
	_, session, _ := newSession(t, modelmcp.Options{ReadOnly: true})
	assert.Equal(t, []string{"test_list"}, session.ToolNames())
}

func TestCallList(t *testing.T) {
	ctx, session, env := newSession(t, modelmcp.Options{})
	env.Add(modelt.TestModel{ID: "1", Name: "a", State: "running"})
	env.Add(modelt.TestModel{ID: "2", Name: "b", State: "stopped"})

	res := mcptest.CallTool(t, ctx, session, "test_list", map[string]any{"filter": []string{"state==running"}, "fields": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "a"}}, mcptest.Resources(t, res))
	assert.Contains(t, mcptest.Text(res), `"name": "a"`)

	res = mcptest.CallTool(t, ctx, session, "test_list", map[string]any{"sort": []string{"-name"}, "fields": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "b"}, {"name": "a"}}, mcptest.Resources(t, res))

	res = mcptest.CallTool(t, ctx, session, "test_list", map[string]any{"keys": []string{"b"}, "fields": []string{"id", "name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"id": "2", "name": "b"}}, mcptest.Resources(t, res))

	res = mcptest.CallTool(t, ctx, session, "test_list", map[string]any{"keys": []string{"missing"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Empty(t, mcptest.Resources(t, res))
}

func TestCallCreate(t *testing.T) {
	ctx, session, env := newSession(t, modelmcp.Options{})

	res := mcptest.CallTool(t, ctx, session, "test_create", map[string]any{
		"set": map[string]any{"name": "fresh", "settings.foo": 7, "settings.bar": "baz"},
	})
	require.False(t, res.IsError, mcptest.Text(res))
	require.Contains(t, env.Store, "fresh")
	assert.Equal(t, 7, env.Store["fresh"].Settings.Foo)
	assert.Equal(t, "baz", env.Store["fresh"].Settings.Bar)
	created := mcptest.Resources(t, res)
	require.Len(t, created, 1)
	assert.Equal(t, "fresh", created[0]["name"])

	res = mcptest.CallTool(t, ctx, session, "test_create", map[string]any{"set": map[string]any{"nope": "x"}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "nope")

	res = mcptest.CallTool(t, ctx, session, "test_create", map[string]any{
		"set":     map[string]any{"name": "preview"},
		"dry_run": true,
	})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Nil(t, res.StructuredContent)
	assert.Contains(t, mcptest.Text(res), "preview")
	assert.NotContains(t, env.Store, "preview")
}

func TestCallEdit(t *testing.T) {
	ctx, session, env := newSession(t, modelmcp.Options{})
	env.Add(modelt.TestModel{ID: "1", Name: "a", Settings: modelt.TestSettings{Foo: 1, Bar: "old"}})

	res := mcptest.CallTool(t, ctx, session, "test_edit", map[string]any{
		"key": "a",
		"set": map[string]any{"settings.bar": "new", "settings.foo": "5"},
	})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "new", env.Store["a"].Settings.Bar)
	assert.Equal(t, 5, env.Store["a"].Settings.Foo)
	updated := mcptest.Resources(t, res)
	require.Len(t, updated, 1)
	assert.Equal(t, "new", updated[0]["settings"].(map[string]any)["bar"])

	res = mcptest.CallTool(t, ctx, session, "test_edit", map[string]any{
		"key":     "a",
		"set":     map[string]any{"settings.bar": "later"},
		"dry_run": true,
	})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "new", env.Store["a"].Settings.Bar)
	assert.Contains(t, mcptest.Text(res), "later")

	res = mcptest.CallTool(t, ctx, session, "test_edit", map[string]any{"key": "missing", "set": map[string]any{"settings.bar": "x"}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "not found")
}

func TestCallDelete(t *testing.T) {
	ctx, session, env := newSession(t, modelmcp.Options{})
	env.Add(modelt.TestModel{ID: "1", Name: "a"})
	env.Add(modelt.TestModel{ID: "2", Name: "b"})

	res := mcptest.CallTool(t, ctx, session, "test_delete", map[string]any{"keys": []string{"a"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.NotContains(t, env.Store, "a")
	assert.Contains(t, env.Store, "b")
	deleted := mcptest.Resources(t, res)
	require.Len(t, deleted, 1)
	assert.Equal(t, "a", deleted[0]["name"])

	res = mcptest.CallTool(t, ctx, session, "test_delete", map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "keys is required")
}

func TestCallLogging(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.TraceLevel)
	env := modelt.NewTestEnv()
	ctx := log.WithLogger(modelt.WithTestEnv(t.Context(), env), &logger)
	session := mcptest.ConnectServer(t, ctx, newServer(t, ctx, modelmcp.Options{}))

	_, _, err := session.Call(ctx, "test_list", json.RawMessage(`{"fields":["name"]}`))
	require.NoError(t, err)
	mcptest.CallTool(t, ctx, session, "test_create", map[string]any{"set": map[string]any{"nope": "x"}})
	mcptest.CallTool(t, ctx, session, "test_delete", map[string]any{})

	out := logs.String()
	assert.Contains(t, out, `"level":"trace","tool":"test_list","arguments":{"fields":["name"]},"message":"calling tool"`)
	assert.Contains(t, out, `"level":"debug","tool":"test_list","took":`)
	assert.Contains(t, out, `"message":"tool call complete"`)
	assert.Contains(t, out, `"tool":"test_create","took":`)
	assert.Contains(t, out, `"error":"unknown fields: [nope]","message":"tool call failed"`)
	assert.Contains(t, out, `"tool":"test_delete","error":"invalid arguments: keys is required","message":"tool call rejected"`)
}
