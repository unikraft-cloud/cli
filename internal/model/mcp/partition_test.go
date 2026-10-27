// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/mcpserver/mcptest"
	"unikraft.com/cli/internal/model"
	modelmcp "unikraft.com/cli/internal/model/mcp"
	modelt "unikraft.com/cli/internal/model/testing"
)

type peerKey string

func (k peerKey) String() string    { return string(k) }
func (k peerKey) Canonical() string { return string(k) }

type peerLink string

func (l peerLink) Link() (string, model.Key, bool) {
	if l == "" {
		return "", nil, false
	}
	return "test", peerKey(l), true
}

type peered struct {
	modelt.TestModel
}

func (peered) Type() model.Type {
	return model.Type{Name: "peered", Names: "peereds"}
}

func (peered) Fields(context.Context) ([]model.Field, error) {
	return []model.Field{
		{Name: "name", Create: &model.Patch{Set: ""}},
		{Name: "peers", Create: &model.Patch{Set: []peerLink{}}},
	}, nil
}

func (peered) Create(context.Context, []model.Field) ([]model.Model, error) {
	return []model.Model{peered{}}, nil
}

type partitionRunner struct {
	ctx       context.Context
	env       *modelt.TestEnv
	partition *model.Partition
}

func newPartitionRunner(t *testing.T, inside ...string) *partitionRunner {
	t.Helper()
	env := modelt.NewTestEnv()
	ctx := modelt.WithTestEnv(t.Context(), env)
	partition, err := model.LoadPartition(filepath.Join(t.TempDir(), "partition.json"), modelt.TestModel{})
	require.NoError(t, err)
	for _, name := range inside {
		m := modelt.TestModel{ID: name, Name: name}
		env.Add(m)
		require.NoError(t, partition.Add(ctx, m))
	}
	require.NoError(t, partition.Save())
	env.Add(modelt.TestModel{ID: "outside", Name: "outside"})
	return &partitionRunner{ctx: ctx, env: env, partition: partition}
}

func (r *partitionRunner) connect(t *testing.T, add func(*modelmcp.Tools) error) *mcptest.Session {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	require.NoError(t, add(modelmcp.New(srv, modelmcp.Options{Partition: r.partition})))
	return mcptest.ConnectServer(t, r.ctx, srv)
}

func (r *partitionRunner) testTools(ts *modelmcp.Tools) error {
	return ts.AddTools[modelt.TestModel](r.ctx)
}

func (r *partitionRunner) call(t *testing.T, session *mcptest.Session, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	return mcptest.CallTool(t, r.ctx, session, name, args)
}

func (r *partitionRunner) saved(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(r.partition.Path)
	require.NoError(t, err)
	var keys map[string][]string
	require.NoError(t, json.Unmarshal(data, &keys))
	return keys["test"]
}

func TestPartitionSyncsEveryCall(t *testing.T) {
	r := newPartitionRunner(t)
	session := r.connect(t, r.testTools)

	res := r.call(t, session, "test_create", map[string]any{"set": map[string]any{"name": "fresh"}})
	require.False(t, res.IsError, mcptest.Text(res))
	require.Len(t, r.saved(t), 1)

	res = r.call(t, session, "test_list", map[string]any{"fields": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "fresh"}}, mcptest.Resources(t, res))

	saved, err := json.Marshal(map[string][]string{"test": append(r.saved(t), r.env.Store["outside"].Key().Canonical())})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(r.partition.Path, saved, 0o644))
	res = r.call(t, session, "test_list", map[string]any{"fields": []string{"name"}, "sort": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "fresh"}, {"name": "outside"}}, mcptest.Resources(t, res))
}

func TestPartitionDelete(t *testing.T) {
	r := newPartitionRunner(t, "inside")
	session := r.connect(t, r.testTools)

	res := r.call(t, session, "test_delete", map[string]any{"keys": []string{"inside", "outside"}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), `test "outside" is outside this server's partition`)
	assert.Contains(t, r.env.Store, "inside")
	assert.Contains(t, r.env.Store, "outside")

	res = r.call(t, session, "test_delete", map[string]any{"keys": []string{"inside"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.NotContains(t, r.env.Store, "inside")
	assert.Empty(t, r.saved(t))
}

func TestPartitionCreateReferences(t *testing.T) {
	r := newPartitionRunner(t, "inside")
	session := r.connect(t, func(ts *modelmcp.Tools) error {
		return ts.AddCreateTool[modelt.TestModel](r.ctx, modelmcp.WithReference[modelt.TestModel]("settings.bar"))
	})

	res := r.call(t, session, "test_create", map[string]any{"set": map[string]any{"name": "copy", "settings.bar": "outside"}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), `test "outside" is outside this server's partition`)
	assert.NotContains(t, r.env.Store, "copy")

	for _, ref := range []string{"inside", "unknown"} {
		res = r.call(t, session, "test_create", map[string]any{"set": map[string]any{"name": "copy-" + ref, "settings.bar": ref}})
		require.False(t, res.IsError, mcptest.Text(res))
		assert.Contains(t, r.env.Store, "copy-"+ref)
	}

	ts := modelmcp.New(mcp.NewServer(&mcp.Implementation{Name: "test"}, nil), modelmcp.Options{})
	err := ts.AddCreateTool[modelt.TestModel](r.ctx, modelmcp.WithReference[modelt.TestModel]("state"))
	assert.EqualError(t, err, `test_create: cannot reference "state": not a creatable field`)
}

func TestPartitionCreateLinks(t *testing.T) {
	r := newPartitionRunner(t, "inside")
	session := r.connect(t, func(ts *modelmcp.Tools) error { return ts.AddCreateTool[peered](r.ctx) })

	res := r.call(t, session, "peered_create", map[string]any{"set": map[string]any{"name": "a", "peers": []string{"inside", "outside"}}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), `test "outside" is outside this server's partition`)

	res = r.call(t, session, "peered_create", map[string]any{"set": map[string]any{"name": "b", "peers": []string{"inside", "unknown"}}})
	require.False(t, res.IsError, mcptest.Text(res))
}

func TestPartitionEdit(t *testing.T) {
	r := newPartitionRunner(t, "inside")
	session := r.connect(t, r.testTools)

	res := r.call(t, session, "test_edit", map[string]any{"key": "outside", "set": map[string]any{"settings.bar": "changed"}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), `test "outside" is outside this server's partition`)
	assert.Empty(t, r.env.Store["outside"].Settings.Bar)

	res = r.call(t, session, "test_edit", map[string]any{"key": "inside", "set": map[string]any{"settings.bar": "changed"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "changed", r.env.Store["inside"].Settings.Bar)
}

func TestPartitionGetTool(t *testing.T) {
	r := newPartitionRunner(t)
	session := r.connect(t, func(ts *modelmcp.Tools) error { return ts.AddGetTool[modelt.TestModel](r.ctx, "Inspect tests.") })
	res := r.call(t, session, "test_get", map[string]any{"keys": []string{"outside"}})
	assert.True(t, res.IsError)

	session = r.connect(t, func(ts *modelmcp.Tools) error {
		return ts.AddGetTool[modelt.TestModel](r.ctx, "Inspect tests.", modelmcp.WithoutPartition())
	})
	res = r.call(t, session, "test_get", map[string]any{"keys": []string{"outside"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "outside", mcptest.Resources(t, res)[0]["name"])
}

func TestPartitionConcurrentCalls(t *testing.T) {
	r := newPartitionRunner(t)
	session := r.connect(t, r.testTools)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			res, err := session.CallTool(r.ctx, &mcp.CallToolParams{
				Name:      "test_create",
				Arguments: map[string]any{"set": map[string]any{"name": fmt.Sprintf("r%d", i)}},
			})
			if assert.NoError(t, err) {
				assert.False(t, res.IsError, mcptest.Text(res))
			}
		})
	}
	wg.Wait()
	assert.Len(t, r.saved(t), 8)
}
