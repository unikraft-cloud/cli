// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"unikraft.com/cli/internal/mcpserver/mcptest"
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

const answer = `{"type": "text", "text": "Done."}`

// newConversation serves the model responses in order and gives a
// conversation over a server with an instance_list tool that returns
// structured content and a metro_list tool that fails, and the requests the
// model received as raw JSON.
func newConversation(t *testing.T, responses ...string) (*Conversation, *[]string) {
	t.Helper()
	var requests []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	t.Cleanup(api.Close)

	srv := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	srv.AddTool(&mcp.Tool{Name: "instance_list", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: `[{"name": "web-1"}]`}},
			StructuredContent: map[string]any{"resources": []any{map[string]any{"name": "web-1"}}},
		}, nil
	})
	srv.AddTool(&mcp.Tool{Name: "metro_list", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var res mcp.CallToolResult
		res.SetError(errors.New("no metros"))
		return &res, nil
	})
	session := mcptest.ConnectServer(t, t.Context(), srv)

	cfg := Config{
		Client:      anthropic.NewClient(option.WithBaseURL(api.URL), option.WithAPIKey("test"), option.WithMaxRetries(0)),
		Model:       "claude-sonnet-5-5",
		Concurrency: 1,
		Timeout:     time.Minute,
		MaxTurns:    3,
	}
	return New(t, session.ClientSession, cfg, "You operate Unikraft Cloud."), &requests
}

func TestConversationSend(t *testing.T) {
	c, requests := newConversation(t,
		message("tool_use", toolUse("t1", "instance_list", `{"keys": ["web-1"]}`)+","+toolUse("t2", "metro_list", `{}`)),
		message("end_turn", answer),
	)

	reply := c.Send("List my instances.")

	assert.Equal(t, "Done.", reply.Text)
	assert.Equal(t, []string{"instance_list", "metro_list"}, reply.Tools())
	list := reply.Called("instance_list")
	require.Len(t, list, 1)
	assert.JSONEq(t, `{"keys": ["web-1"]}`, string(list[0].Args))
	assert.JSONEq(t, `{"resources": [{"name": "web-1"}]}`, string(list[0].Result))
	assert.False(t, list[0].IsError)
	metros := reply.Called("metro_list")
	require.Len(t, metros, 1)
	assert.True(t, metros[0].IsError)
	assert.Equal(t, "no metros", metros[0].Text)
	assert.Nil(t, metros[0].Result)

	assert.Equal(t, Usage{InputTokens: 20, OutputTokens: 10, CacheReadInputTokens: 6}, c.usage)
	assert.Equal(t, []string{"system", "user", "tool_call", "tool_call", "tool_result", "tool_result", "assistant"}, roles(c.trace))
	assert.Equal(t, "error: no metros", c.trace[5].Content)

	require.Len(t, *requests, 2)
	first, second := (*requests)[0], (*requests)[1]
	assert.Equal(t, "claude-sonnet-5-5", gjson.Get(first, "model").String())
	assert.Equal(t, "You operate Unikraft Cloud.", gjson.Get(first, "system.0.text").String())
	assert.Equal(t, "ephemeral", gjson.Get(first, "system.0.cache_control.type").String())
	assert.ElementsMatch(t, []string{"instance_list", "metro_list"}, toStrings(gjson.Get(first, "tools.#.name").Array()))
	assert.Equal(t, "List my instances.", gjson.Get(first, "messages.0.content.0.text").String())
	assert.Equal(t, int64(3), gjson.Get(second, "messages.#").Int())
	assert.Equal(t, "t1", gjson.Get(second, "messages.2.content.0.tool_use_id").String())
	assert.True(t, gjson.Get(second, "messages.2.content.1.is_error").Bool())
}

func TestConversationFollowUp(t *testing.T) {
	c, requests := newConversation(t,
		message("end_turn", `{"type": "text", "text": "Two metros."}`),
		message("tool_use", toolUse("t1", "instance_list", `{}`)),
		message("end_turn", answer),
	)

	assert.Equal(t, "Two metros.", c.Send("How many metros are there?").Text)
	reply := c.Send("List my instances.")
	assert.Equal(t, []string{"instance_list"}, reply.Tools())
	assert.Equal(t, "Done.", reply.Text)

	require.Len(t, *requests, 3)
	second := (*requests)[1]
	assert.Equal(t, int64(3), gjson.Get(second, "messages.#").Int())
	assert.Equal(t, "List my instances.", gjson.Get(second, "messages.2.content.0.text").String())
}

func TestConversationEndings(t *testing.T) {
	t.Run("refusal", func(t *testing.T) {
		c, _ := newConversation(t, message("refusal", ""))
		_, err := c.send(t.Context(), "Hello.")
		require.EqualError(t, err, "the model refused (cyber): ")
		assert.Equal(t, statusRefusal, c.status)
	})
	t.Run("truncated", func(t *testing.T) {
		c, _ := newConversation(t, message("max_tokens", answer))
		_, err := c.send(t.Context(), "Hello.")
		require.ErrorContains(t, err, "cut off")
		assert.Equal(t, statusTruncated, c.status)
	})
	t.Run("max-turns", func(t *testing.T) {
		loop := message("tool_use", toolUse("t1", "instance_list", `{}`))
		c, _ := newConversation(t, loop, loop, loop)
		reply, err := c.send(t.Context(), "Hello.")
		require.EqualError(t, err, "the model did not answer within 3 turns")
		assert.Len(t, reply.Calls, 3)
		assert.Equal(t, statusMaxTurns, c.status)
	})
}

func roles(trace []Turn) []string {
	out := make([]string, 0, len(trace))
	for _, turn := range trace {
		out = append(out, turn.Role)
	}
	return out
}

func toStrings(results []gjson.Result) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.String())
	}
	return out
}
