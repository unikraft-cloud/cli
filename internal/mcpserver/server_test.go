// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/mcpserver"
	"unikraft.com/cli/internal/mcpserver/mcptest"
	"unikraft.com/cli/internal/resource"
	resourcet "unikraft.com/cli/internal/resource/testing"
)

func newServer(t *testing.T, ctx context.Context, opts mcpserver.Options, add ...func(*mcpserver.Server) error) *mcpserver.Server {
	t.Helper()
	srv, err := mcpserver.New(ctx, opts)
	require.NoError(t, err)
	for _, add := range add {
		require.NoError(t, add(srv))
	}
	return srv
}

func testTools(ctx context.Context) func(*mcpserver.Server) error {
	return func(s *mcpserver.Server) error { return s.AddTools[resourcet.TestResource](ctx) }
}

func newSession(t *testing.T, opts mcpserver.Options) (context.Context, *mcptest.Session, *resourcet.TestEnv) {
	t.Helper()
	env := resourcet.NewTestEnv()
	ctx := resourcet.WithTestEnv(t.Context(), env)
	return ctx, mcptest.ConnectServer(t, ctx, newServer(t, ctx, opts, testTools(ctx))), env
}

func call(t *testing.T, ctx context.Context, session *mcptest.Session, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	return res
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

func property(t *testing.T, schema any, name string) map[string]any {
	t.Helper()
	prop, ok := properties(t, schema)[name].(map[string]any)
	require.True(t, ok, "property %s is not an object", name)
	return prop
}

func TestResourcesAllowlist(t *testing.T) {
	ctx := resourcet.WithTestEnv(t.Context(), resourcet.NewTestEnv())

	srv := newServer(t, ctx, mcpserver.Options{Resources: []string{"Test"}}, testTools(ctx))
	session := mcptest.ConnectServer(t, ctx, srv)
	assert.Len(t, session.Tools, 4)
	assert.Contains(t, session.Instructions, "Only the tools of these resource types are served: test.")

	srv = newServer(t, ctx, mcpserver.Options{Resources: []string{"other-thing"}}, testTools(ctx))
	assert.Empty(t, mcptest.ConnectServer(t, ctx, srv).Tools)
}

func TestListTools(t *testing.T) {
	_, session, _ := newSession(t, mcpserver.Options{})
	assert.ElementsMatch(t, []string{"test_list", "test_create", "test_edit", "test_delete"}, session.ToolNames())

	list := session.Tool("test_list")
	assert.True(t, list.Annotations.ReadOnlyHint)
	assert.Equal(t, "List tests", list.Title)
	assert.NotContains(t, list.Description, "settings.foo")
	filter := property(t, list.InputSchema, "filter")["description"]
	assert.Contains(t, filter, "settings.foo")
	assert.Contains(t, filter, "Field meanings: state: Lifecycle state: running or stopped.")

	create := session.Tool("test_create")
	assert.False(t, create.Annotations.ReadOnlyHint)
	assert.Equal(t, "Create test", create.Title)
	assert.True(t, strings.HasPrefix(create.Description, "Create a test. "), create.Description)
	assert.Contains(t, create.Description, "Required: none.")
	set := properties(t, property(t, create.InputSchema, "set"))
	assert.ElementsMatch(t, []string{"name", "create_only", "settings.foo", "settings.bar"}, slices.Collect(maps.Keys(set)))
	foo := set["settings.foo"].(map[string]any)
	assert.Equal(t, []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}, foo["anyOf"])
	assert.Equal(t, "Foo setting.", foo["description"])

	edit := session.Tool("test_edit")
	assert.False(t, *edit.Annotations.DestructiveHint)
	assert.Equal(t, []any{"key"}, edit.InputSchema.(map[string]any)["required"])
	editSet := properties(t, property(t, edit.InputSchema, "set"))
	assert.ElementsMatch(t, []string{"edit_only", "settings.foo", "settings.bar"}, slices.Collect(maps.Keys(editSet)))

	del := session.Tool("test_delete")
	assert.True(t, *del.Annotations.DestructiveHint)
	assert.Equal(t, []any{"keys"}, del.InputSchema.(map[string]any)["required"])
}

func TestReadOnly(t *testing.T) {
	_, session, _ := newSession(t, mcpserver.Options{ReadOnly: true})
	assert.Equal(t, []string{"test_list"}, session.ToolNames())
}

func TestCallList(t *testing.T) {
	ctx, session, env := newSession(t, mcpserver.Options{})
	env.Add(resourcet.TestResource{ID: "1", Name: "a", State: "running"})
	env.Add(resourcet.TestResource{ID: "2", Name: "b", State: "stopped"})

	res := call(t, ctx, session, "test_list", map[string]any{"filter": []string{"state==running"}, "fields": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "a"}}, resources(t, res))
	assert.Contains(t, mcptest.Text(res), `"name": "a"`)

	res = call(t, ctx, session, "test_list", map[string]any{"sort": []string{"-name"}, "fields": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "b"}, {"name": "a"}}, resources(t, res))

	res = call(t, ctx, session, "test_list", map[string]any{"keys": []string{"b"}, "fields": []string{"id", "name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"id": "2", "name": "b"}}, resources(t, res))

	res = call(t, ctx, session, "test_list", map[string]any{"keys": []string{"missing"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Empty(t, resources(t, res))
}

func TestCallSyncsPartition(t *testing.T) {
	env := resourcet.NewTestEnv()
	ctx := resourcet.WithTestEnv(t.Context(), env)
	path := filepath.Join(t.TempDir(), "partition.json")
	partition, err := resource.LoadPartition(path, resourcet.TestResource{})
	require.NoError(t, err)
	session := mcptest.ConnectServer(t, ctx, newServer(t, ctx, mcpserver.Options{Partition: partition}, testTools(ctx)))

	res := call(t, ctx, session, "test_create", map[string]any{"set": map[string]any{"name": "fresh"}})
	require.False(t, res.IsError, mcptest.Text(res))
	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	var keys map[string][]string
	require.NoError(t, json.Unmarshal(saved, &keys))
	require.Len(t, keys["test"], 1)

	outside := resourcet.TestResource{ID: "9", Name: "outside"}
	env.Add(outside)
	res = call(t, ctx, session, "test_list", map[string]any{"fields": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "fresh"}}, resources(t, res))

	keys["test"] = append(keys["test"], outside.Key().Canonical())
	saved, err = json.Marshal(keys)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, saved, 0o644))
	res = call(t, ctx, session, "test_list", map[string]any{"fields": []string{"name"}, "sort": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "fresh"}, {"name": "outside"}}, resources(t, res))
}

func TestCallCreate(t *testing.T) {
	ctx, session, env := newSession(t, mcpserver.Options{})

	res := call(t, ctx, session, "test_create", map[string]any{
		"set": map[string]any{"name": "fresh", "settings.foo": 7, "settings.bar": "baz"},
	})
	require.False(t, res.IsError, mcptest.Text(res))
	require.Contains(t, env.Store, "fresh")
	assert.Equal(t, 7, env.Store["fresh"].Settings.Foo)
	assert.Equal(t, "baz", env.Store["fresh"].Settings.Bar)
	created := resources(t, res)
	require.Len(t, created, 1)
	assert.Equal(t, "fresh", created[0]["name"])

	res = call(t, ctx, session, "test_create", map[string]any{"set": map[string]any{"nope": "x"}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "nope")

	res = call(t, ctx, session, "test_create", map[string]any{
		"set":     map[string]any{"name": "preview"},
		"dry_run": true,
	})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Nil(t, res.StructuredContent)
	assert.Contains(t, mcptest.Text(res), "preview")
	assert.NotContains(t, env.Store, "preview")
}

func TestCallEdit(t *testing.T) {
	ctx, session, env := newSession(t, mcpserver.Options{})
	env.Add(resourcet.TestResource{ID: "1", Name: "a", Settings: resourcet.TestSettings{Foo: 1, Bar: "old"}})

	res := call(t, ctx, session, "test_edit", map[string]any{
		"key": "a",
		"set": map[string]any{"settings.bar": "new", "settings.foo": "5"},
	})
	require.False(t, res.IsError, mcptest.Text(res))
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
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "new", env.Store["a"].Settings.Bar)
	assert.Contains(t, mcptest.Text(res), "later")

	res = call(t, ctx, session, "test_edit", map[string]any{"key": "missing", "set": map[string]any{"settings.bar": "x"}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "not found")
}

func TestCallDelete(t *testing.T) {
	ctx, session, env := newSession(t, mcpserver.Options{})
	env.Add(resourcet.TestResource{ID: "1", Name: "a"})
	env.Add(resourcet.TestResource{ID: "2", Name: "b"})

	res := call(t, ctx, session, "test_delete", map[string]any{"keys": []string{"a"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.NotContains(t, env.Store, "a")
	assert.Contains(t, env.Store, "b")
	deleted := resources(t, res)
	require.Len(t, deleted, 1)
	assert.Equal(t, "a", deleted[0]["name"])

	res = call(t, ctx, session, "test_delete", map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), "keys is required")
}

func TestCallLogging(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.TraceLevel)
	env := resourcet.NewTestEnv()
	ctx := log.WithLogger(resourcet.WithTestEnv(t.Context(), env), &logger)
	session := mcptest.ConnectServer(t, ctx, newServer(t, ctx, mcpserver.Options{}, testTools(ctx)))

	_, _, err := session.Call(ctx, "test_list", json.RawMessage(`{"fields":["name"]}`))
	require.NoError(t, err)
	call(t, ctx, session, "test_create", map[string]any{"set": map[string]any{"nope": "x"}})
	call(t, ctx, session, "test_delete", map[string]any{})

	out := logs.String()
	assert.Contains(t, out, `"level":"trace","tool":"test_list","arguments":{"fields":["name"]},"message":"calling tool"`)
	assert.Contains(t, out, `"level":"debug","tool":"test_list","took":`)
	assert.Contains(t, out, `"message":"tool call complete"`)
	assert.Contains(t, out, `"tool":"test_create","took":`)
	assert.Contains(t, out, `"error":"unknown fields: [nope]","message":"tool call failed"`)
	assert.Contains(t, out, `"tool":"test_delete","error":"invalid arguments: keys is required","message":"tool call rejected"`)
}
