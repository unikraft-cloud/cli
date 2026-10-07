// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"unikraft.com/cli/internal/eval"
)

func message(stopReason, content string) string {
	return fmt.Sprintf(`{
		"id": "msg", "type": "message", "role": "assistant", "model": "claude-sonnet-5-5-20260101",
		"stop_reason": %q, "stop_details": {"type": "refusal", "category": "cyber", "explanation": ""},
		"content": [%s],
		"usage": {"input_tokens": 10, "output_tokens": 5, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 0}
	}`, stopReason, content)
}

func toolUse(id, name, input string) string {
	return fmt.Sprintf(`{"type": "tool_use", "id": %q, "name": %q, "input": %s}`, id, name, input)
}

// fakeMessages serves the responses in order and gives the requests it
// received, as raw JSON.
func fakeMessages(t *testing.T, responses ...string) (anthropic.Client, *[]string) {
	t.Helper()
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests = append(requests, string(body))
		if len(requests) > len(responses) {
			http.Error(w, "no response left", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[len(requests)-1]))
	}))
	t.Cleanup(srv.Close)
	return anthropic.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0)), &requests
}

func newAgent(client anthropic.Client, exec eval.Exec) *eval.Agent {
	return &eval.Agent{
		Client: client,
		Config: eval.Config{Model: "claude-sonnet-5-5", MaxTurns: 3, Concurrency: 1},
		System: "You operate Unikraft Cloud.",
		Tools:  []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{Name: "instance_list", InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{}}}}},
		Exec:   exec,
	}
}

func TestAgentRun(t *testing.T) {
	client, requests := fakeMessages(t,
		message("tool_use", toolUse("t1", "instance_list", `{"keys": ["web-1"]}`)+","+toolUse("t2", "metro_list", `{}`)),
		message("tool_use", toolUse("t3", "instance_delete", `{"keys": ["web-1"]}`)+","+toolUse("t4", "instance_list", `{}`)),
	)
	var executed []string
	agent := newAgent(client, func(_ context.Context, name string, args json.RawMessage) (string, bool, error) {
		executed = append(executed, name)
		switch name {
		case "instance_delete":
			return "", false, eval.ErrStop
		case "metro_list":
			return "no metros", true, nil
		}
		return "", false, nil
	})

	res, err := agent.Run(t.Context(), "Remove web-1.")
	require.NoError(t, err)

	assert.Equal(t, eval.StatusOK, res.Status)
	assert.Equal(t, "tool_use", res.StopReason)
	assert.Equal(t, "claude-sonnet-5-5-20260101", res.Model)
	assert.Equal(t, []string{"instance_list", "metro_list", "instance_delete"}, res.Tools())
	assert.Equal(t, []string{"instance_list", "metro_list", "instance_delete"}, executed)
	assert.True(t, res.Steps[1].IsError)
	assert.Equal(t, "no metros", res.Steps[1].Result)
	assert.Equal(t, eval.Usage{InputTokens: 20, OutputTokens: 10, CacheReadInputTokens: 6}, res.Usage)
	assert.Equal(t, []string{"system", "user", "tool_call", "tool_call", "tool_result", "tool_result", "tool_call", "tool_call"}, roles(res.Trace))
	assert.Equal(t, "(no output)", res.Trace[4].Content)
	assert.Equal(t, "error: no metros", res.Trace[5].Content)

	require.Len(t, *requests, 2)
	first, second := (*requests)[0], (*requests)[1]
	assert.Equal(t, "claude-sonnet-5-5", gjson.Get(first, "model").String())
	assert.Equal(t, "You operate Unikraft Cloud.", gjson.Get(first, "system.0.text").String())
	assert.Equal(t, "ephemeral", gjson.Get(first, "system.0.cache_control.type").String())
	assert.Equal(t, "instance_list", gjson.Get(first, "tools.0.name").String())
	assert.Equal(t, "Remove web-1.", gjson.Get(first, "messages.0.content.0.text").String())

	assert.Equal(t, int64(3), gjson.Get(second, "messages.#").Int())
	assert.Equal(t, int64(2), gjson.Get(second, "messages.2.content.#").Int())
	assert.Equal(t, "t1", gjson.Get(second, "messages.2.content.0.tool_use_id").String())
	assert.Contains(t, gjson.Get(second, "messages.2.content.0.content").Raw, "(no output)")
	assert.True(t, gjson.Get(second, "messages.2.content.1.is_error").Bool())
}

func TestAgentRunWithoutExecutor(t *testing.T) {
	client, requests := fakeMessages(t,
		message("tool_use", `{"type": "text", "text": "Listing."},`+toolUse("t1", "instance_list", `{}`)),
	)

	res, err := newAgent(client, nil).Run(t.Context(), "List my instances.")
	require.NoError(t, err)

	assert.Equal(t, eval.StatusOK, res.Status)
	assert.Equal(t, []string{"instance_list"}, res.Tools())
	assert.Equal(t, "Listing.", res.Text)
	assert.Len(t, *requests, 1)
}

func TestAgentRunConversation(t *testing.T) {
	client, requests := fakeMessages(t,
		message("end_turn", `{"type": "text", "text": "Two metros."}`),
		message("tool_use", toolUse("t1", "instance_list", `{}`)),
	)

	res, err := newAgent(client, nil).Run(t.Context(), "How many metros are there?", "List my instances.")
	require.NoError(t, err)

	assert.Equal(t, eval.StatusOK, res.Status)
	assert.Equal(t, []string{"instance_list"}, res.Tools())
	assert.Equal(t, []string{"system", "user", "assistant", "user", "tool_call"}, roles(res.Trace))
	require.Len(t, *requests, 2)
	second := (*requests)[1]
	assert.Equal(t, int64(3), gjson.Get(second, "messages.#").Int())
	assert.Equal(t, "List my instances.", gjson.Get(second, "messages.2.content.0.text").String())

	_, err = newAgent(client, nil).Run(t.Context())
	require.EqualError(t, err, "no prompt")
}

func TestAgentRunEndings(t *testing.T) {
	answer := `{"type": "text", "text": "Done."}`
	tests := map[string]struct {
		responses []string
		status    string
		refusal   string
	}{
		"end-turn": {
			responses: []string{message("end_turn", answer)},
			status:    eval.StatusOK,
		},
		"refusal": {
			responses: []string{message("refusal", "")},
			status:    eval.StatusRefusal,
			refusal:   "cyber",
		},
		"truncated": {
			responses: []string{message("max_tokens", answer)},
			status:    eval.StatusTruncated,
		},
		"max-turns": {
			responses: []string{
				message("tool_use", toolUse("t1", "instance_list", `{}`)),
				message("tool_use", toolUse("t2", "instance_list", `{}`)),
				message("tool_use", toolUse("t3", "instance_list", `{}`)),
			},
			status: eval.StatusMaxTurns,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client, _ := fakeMessages(t, tt.responses...)
			agent := newAgent(client, func(context.Context, string, json.RawMessage) (string, bool, error) {
				return "[]", false, nil
			})

			res, err := agent.Run(t.Context(), "Hello.")
			require.NoError(t, err)
			assert.Equal(t, tt.status, res.Status)
			assert.Equal(t, tt.refusal, res.Refusal)
		})
	}
}

func roles(trace []eval.Turn) []string {
	out := make([]string, 0, len(trace))
	for _, turn := range trace {
		out = append(out, turn.Role)
	}
	return out
}
