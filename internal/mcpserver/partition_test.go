// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

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

	"unikraft.com/cli/internal/mcpserver"
	"unikraft.com/cli/internal/mcpserver/mcptest"
	"unikraft.com/cli/internal/resource"
	resourcet "unikraft.com/cli/internal/resource/testing"
)

type peerKey string

func (k peerKey) String() string    { return string(k) }
func (k peerKey) Canonical() string { return string(k) }

type peerLink string

func (l peerLink) Link() (string, resource.Key, bool) {
	if l == "" {
		return "", nil, false
	}
	return "test", peerKey(l), true
}

type peered struct {
	resourcet.TestResource
}

func (peered) Type() resource.Type {
	return resource.Type{Name: "peered", Names: "peereds"}
}

func (peered) Fields(context.Context) ([]resource.Field, error) {
	return []resource.Field{
		{Name: "name", Create: &resource.Patch{Set: ""}},
		{Name: "peers", Create: &resource.Patch{Set: []peerLink{}}},
	}, nil
}

func (peered) Create(context.Context, []resource.Field) ([]resource.Resource, error) {
	return []resource.Resource{peered{}}, nil
}

type partitionRunner struct {
	ctx       context.Context
	env       *resourcet.TestEnv
	partition *resource.Partition
}

func newPartitionRunner(t *testing.T, inside ...string) *partitionRunner {
	t.Helper()
	env := resourcet.NewTestEnv()
	ctx := resourcet.WithTestEnv(t.Context(), env)
	partition, err := resource.LoadPartition(filepath.Join(t.TempDir(), "partition.json"), resourcet.TestResource{})
	require.NoError(t, err)
	for _, name := range inside {
		r := resourcet.TestResource{ID: name, Name: name}
		env.Add(r)
		require.NoError(t, partition.Add(ctx, r))
	}
	require.NoError(t, partition.Save())
	env.Add(resourcet.TestResource{ID: "outside", Name: "outside"})
	return &partitionRunner{ctx: ctx, env: env, partition: partition}
}

func (r *partitionRunner) connect(t *testing.T, add ...func(*mcpserver.Server) error) *mcptest.Session {
	t.Helper()
	return mcptest.ConnectServer(t, r.ctx, newServer(t, r.ctx, mcpserver.Options{Partition: r.partition}, add...))
}

func (r *partitionRunner) call(t *testing.T, session *mcptest.Session, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	return call(t, r.ctx, session, name, args)
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
	session := r.connect(t, testTools(r.ctx))

	res := r.call(t, session, "test_create", map[string]any{"set": map[string]any{"name": "fresh"}})
	require.False(t, res.IsError, mcptest.Text(res))
	require.Len(t, r.saved(t), 1)

	res = r.call(t, session, "test_list", map[string]any{"fields": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "fresh"}}, resources(t, res))

	saved, err := json.Marshal(map[string][]string{"test": append(r.saved(t), r.env.Store["outside"].Key().Canonical())})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(r.partition.Path, saved, 0o644))
	res = r.call(t, session, "test_list", map[string]any{"fields": []string{"name"}, "sort": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "fresh"}, {"name": "outside"}}, resources(t, res))
}

func TestPartitionDelete(t *testing.T) {
	r := newPartitionRunner(t, "inside")
	session := r.connect(t, testTools(r.ctx))

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

func TestPartitionInstanceTools(t *testing.T) {
	r := newPartitionRunner(t, "web")
	fake := &fakeInstances{}
	session := r.connect(t, instanceTools(fake))

	res := r.call(t, session, "instance_control", map[string]any{"action": "stop", "keys": []string{"web", "outside"}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), `test "outside" is outside this server's partition`)

	res = r.call(t, session, "instance_logs", map[string]any{"key": "outside"})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), `test "outside" is outside this server's partition`)

	res = r.call(t, session, "instance_logs", map[string]any{"key": "missing"})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), `test "missing" not found`)

	res = r.call(t, session, "instance_control", map[string]any{"action": "stop", "keys": []string{"web"}})
	require.False(t, res.IsError, mcptest.Text(res))
	res = r.call(t, session, "instance_logs", map[string]any{"key": "web"})
	require.False(t, res.IsError, mcptest.Text(res))

	assert.Equal(t, []string{"stop web force=false drain=-1", "logs web tail=200"}, fake.calls)
}

func TestPartitionCreateReferences(t *testing.T) {
	r := newPartitionRunner(t, "inside")
	session := r.connect(t, func(s *mcpserver.Server) error {
		return s.AddCreateTool[resourcet.TestResource](r.ctx, mcpserver.WithReference[resourcet.TestResource]("settings.bar"))
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

	srv, err := mcpserver.New(r.ctx, mcpserver.Options{})
	require.NoError(t, err)
	err = srv.AddCreateTool[resourcet.TestResource](r.ctx, mcpserver.WithReference[resourcet.TestResource]("state"))
	assert.EqualError(t, err, `test_create: cannot reference "state": not a creatable field`)
}

func TestPartitionCreateLinks(t *testing.T) {
	r := newPartitionRunner(t, "inside")
	session := r.connect(t, func(s *mcpserver.Server) error { return s.AddCreateTool[peered](r.ctx) })

	res := r.call(t, session, "peered_create", map[string]any{"set": map[string]any{"name": "a", "peers": []string{"inside", "outside"}}})
	assert.True(t, res.IsError)
	assert.Contains(t, mcptest.Text(res), `test "outside" is outside this server's partition`)

	res = r.call(t, session, "peered_create", map[string]any{"set": map[string]any{"name": "b", "peers": []string{"inside", "unknown"}}})
	require.False(t, res.IsError, mcptest.Text(res))
}

func TestPartitionEdit(t *testing.T) {
	r := newPartitionRunner(t, "inside")
	session := r.connect(t, testTools(r.ctx))

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
	session := r.connect(t, func(s *mcpserver.Server) error { return s.AddGetTool[resourcet.TestResource](r.ctx, "Inspect tests.") })
	res := r.call(t, session, "test_get", map[string]any{"keys": []string{"outside"}})
	assert.True(t, res.IsError)

	session = r.connect(t, func(s *mcpserver.Server) error {
		return s.AddGetTool[resourcet.TestResource](r.ctx, "Inspect tests.", mcpserver.WithoutPartition())
	})
	res = r.call(t, session, "test_get", map[string]any{"keys": []string{"outside"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, "outside", resources(t, res)[0]["name"])
}

func TestPartitionConcurrentCalls(t *testing.T) {
	r := newPartitionRunner(t)
	session := r.connect(t, testTools(r.ctx))

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
