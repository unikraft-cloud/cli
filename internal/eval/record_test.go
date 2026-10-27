// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/eval"
)

func TestRecord(t *testing.T) {
	dir := t.TempDir()
	recorder := eval.NewRecorder(dir)

	row := eval.Row{
		PromptID: "stop one",
		Prompt:   "Stop web-1.",
		Tags:     []string{"ToolSelection"},
		Model:    "claude-sonnet-5-5",
		Status:   "ok",
		Grade:    map[string]float64{"pass": 1},
		Usage:    eval.Usage{InputTokens: 10, CacheReadInputTokens: 5},
	}
	trace := []eval.Turn{{Role: "user", Content: "Stop web-1."}, {Role: "tool_call", Name: "instance_control", Content: "{}"}}

	path, err := recorder.Record(row, trace)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "traces", "stop_one_rep0.json"), path)

	path, err = recorder.Record(row, trace)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "traces", "stop_one_rep1.json"), path)

	other, err := recorder.Record(eval.Row{PromptID: "other"}, trace)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "traces", "other_rep0.json"), other)

	results, err := os.ReadFile(filepath.Join(dir, "results.jsonl"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(results)), "\n")
	require.Len(t, lines, 3)
	assert.JSONEq(t, `{
		"prompt_id": "stop one", "rep": 0, "prompt": "Stop web-1.", "tags": ["ToolSelection"],
		"model": "claude-sonnet-5-5", "stop_reason": "", "status": "ok", "grade": {"pass": 1},
		"latency_s": 0, "tool_calls": 0,
		"usage": {"input_tokens": 10, "output_tokens": 0, "cache_read_input_tokens": 5, "cache_creation_input_tokens": 0}
	}`, lines[0])

	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `[
		{"role": "user", "content": "Stop web-1."},
		{"role": "tool_call", "name": "instance_control", "content": "{}"}
	]`, string(saved))
}
