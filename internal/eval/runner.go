// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Runner runs prompts through an agent as subtests and records every run.
type Runner struct {
	Agent  *Agent
	Config Config

	recorder *Recorder
}

func NewRunner(agent *Agent, cfg Config) *Runner {
	return &Runner{Agent: agent, Config: cfg, recorder: NewRecorder(cfg.OutDir)}
}

// Prompt runs the prompt once per configured rep, each as a parallel subtest
// of name, and calls check on every result. Every rep records a row and its
// transcript, graded by whether the subtest passed.
func (r *Runner) Prompt(t *testing.T, name, prompt string, check func(t *testing.T, res *Result)) {
	t.Helper()
	r.Conversation(t, name, []string{prompt}, check)
}

// Conversation is Prompt for several prompts sent in turn, each once the
// model has answered the previous one.
func (r *Runner) Conversation(t *testing.T, name string, prompts []string, check func(t *testing.T, res *Result)) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Parallel()
		if r.Config.Reps <= 1 {
			r.rep(t, name, prompts, 0, check)
			return
		}
		for k := range r.Config.Reps {
			t.Run(fmt.Sprintf("rep%d", k), func(t *testing.T) {
				t.Parallel()
				r.rep(t, name, prompts, k, check)
			})
		}
	})
}

func (r *Runner) rep(t *testing.T, name string, prompts []string, k int, check func(t *testing.T, res *Result)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), r.Config.Timeout)
	defer cancel()

	res, err := r.Agent.Run(ctx, prompts...)
	require.NoError(t, err)
	defer r.record(t, name, strings.Join(prompts, "\n\n"), k, res)

	require.Equal(t, StatusOK, res.Status, "run ended with %s; the reply was: %s", res.StopReason, res.Text)
	assert.True(t, strings.HasPrefix(res.Model, r.Config.Model), "served by %s instead of %s", res.Model, r.Config.Model)
	check(t, res)
}

func (r *Runner) record(t *testing.T, name, prompt string, k int, res *Result) {
	t.Helper()
	row := Row{
		PromptID:   name,
		Rep:        k,
		Prompt:     prompt,
		Tags:       []string{suite(t.Name())},
		Model:      res.Model,
		StopReason: res.StopReason,
		Status:     res.Status,
		Grade:      map[string]float64{"pass": grade(!t.Failed())},
		LatencyS:   res.Latency.Seconds(),
		ToolCalls:  len(res.Steps),
		Usage:      res.Usage,
	}
	if res.Refusal != "" {
		row.Meta = map[string]any{"refusal": res.Refusal}
	}
	path, err := r.recorder.Record(row, res.Trace)
	require.NoError(t, err)
	if t.Failed() {
		t.Logf("transcript: %s", path)
	}
}

func grade(pass bool) float64 {
	if pass {
		return 1
	}
	return 0
}

func suite(testName string) string {
	name, _, _ := strings.Cut(testName, "/")
	return strings.TrimPrefix(name, "Test")
}
