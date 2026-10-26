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
