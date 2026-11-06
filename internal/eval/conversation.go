// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"
)

const (
	statusOK        = "ok"
	statusTruncated = "truncated"
	statusRefusal   = "refusal"
	statusMaxTurns  = "max_turns"

	maxTokens = 4096
	noOutput  = "(no output)"
)

// Conversation drives a model through the tools of an MCP session, one
// prompt at a time, and records its transcript when the test ends.
type Conversation struct {
	t       testing.TB
	session *mcp.ClientSession
	cfg     Config
	system  string
	tools   []anthropic.ToolUnionParam

	messages []anthropic.MessageParam
	prompts  []string
	trace    []Turn
	calls    int
	usage    Usage
	latency  time.Duration
	model    string
	stop     string
	status   string
	refusal  string
}

// Reply is what the model did with one prompt.
type Reply struct {
	// Text is the assistant text of the last turn.
	Text  string
	Calls []Call
}

// Call is one tool call and what the tool returned.
type Call struct {
	Name string
	Args json.RawMessage
	// Result is the structured content of the result, nil when the tool
	// returned only text.
	Result  json.RawMessage
	Text    string
	IsError bool
}

var (
	limiterOnce sync.Once
	limiter     *semaphore.Weighted
)

func New(t testing.TB, session *mcp.ClientSession, cfg Config, system string) *Conversation {
	t.Helper()
	res, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	tools, err := ToolParams(res.Tools)
	require.NoError(t, err)
	c := &Conversation{
		t:       t,
		session: session,
		cfg:     cfg,
		system:  system,
		tools:   tools,
		trace:   []Turn{{Role: "system", Content: system}},
	}
	t.Cleanup(c.record)
	return c
}

// Send sends the prompt, runs the tool calls the model makes until it
// answers, and fails the test unless it answered normally.
func (c *Conversation) Send(prompt string) Reply {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(c.t.Context(), c.cfg.Timeout)
	defer cancel()
	start := time.Now()
	reply, err := c.send(ctx, prompt)
	c.latency += time.Since(start)
	require.NoError(c.t, err)
	assert.True(c.t, strings.HasPrefix(c.model, c.cfg.Model), "served by %s instead of %s", c.model, c.cfg.Model)
	return reply
}

// Tools gives the names of the called tools in order.
func (r Reply) Tools() []string {
	names := make([]string, 0, len(r.Calls))
	for _, call := range r.Calls {
		names = append(names, call.Name)
	}
	return names
}

// Called gives the calls of the tool name in order.
func (r Reply) Called(name string) []Call {
	var calls []Call
	for _, call := range r.Calls {
		if call.Name == name {
			calls = append(calls, call)
		}
	}
	return calls
}

func (c *Conversation) send(ctx context.Context, prompt string) (Reply, error) {
	c.prompts = append(c.prompts, prompt)
	c.trace = append(c.trace, Turn{Role: "user", Content: prompt})
	c.messages = append(c.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)))
	var reply Reply
	for range c.cfg.MaxTurns {
		msg, err := c.request(ctx)
		if err != nil {
			return reply, err
		}
		c.messages = append(c.messages, msg.ToParam())
		c.model, c.stop = msg.Model, string(msg.StopReason)
		c.usage.add(msg.Usage)

		var texts []string
		var uses []anthropic.ToolUseBlock
		for _, block := range msg.Content {
			switch v := block.AsAny().(type) {
			case anthropic.TextBlock:
				texts = append(texts, v.Text)
				c.trace = append(c.trace, Turn{Role: "assistant", Content: v.Text})
			case anthropic.ToolUseBlock:
				uses = append(uses, v)
				c.trace = append(c.trace, Turn{Role: "tool_call", Name: v.Name, Content: pretty(v.Input)})
			}
		}
		reply.Text = strings.Join(texts, "\n")

		switch msg.StopReason {
		case anthropic.StopReasonRefusal:
			c.status, c.refusal = statusRefusal, string(msg.StopDetails.Category)
			return reply, fmt.Errorf("the model refused (%s): %s", c.refusal, reply.Text)
		case anthropic.StopReasonMaxTokens:
			c.status = statusTruncated
			return reply, fmt.Errorf("the reply was cut off at %d tokens: %s", maxTokens, reply.Text)
		case anthropic.StopReasonToolUse:
			results := make([]anthropic.ContentBlockParamUnion, 0, len(uses))
			for _, use := range uses {
				call, err := c.call(ctx, use)
				if err != nil {
					return reply, err
				}
				reply.Calls = append(reply.Calls, call)
				results = append(results, anthropic.NewToolResultBlock(use.ID, cmp.Or(call.Text, noOutput), call.IsError))
			}
			c.messages = append(c.messages, anthropic.NewUserMessage(results...))
		default:
			c.status = statusOK
			return reply, nil
		}
	}
	c.status = statusMaxTurns
	return reply, fmt.Errorf("the model did not answer within %d turns", c.cfg.MaxTurns)
}

func (c *Conversation) call(ctx context.Context, use anthropic.ToolUseBlock) (Call, error) {
	c.calls++
	call := Call{Name: use.Name, Args: json.RawMessage(use.Input)}
	res, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: use.Name, Arguments: call.Args})
	if err != nil {
		return call, fmt.Errorf("calling %s: %w", use.Name, err)
	}
	var texts []string
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			texts = append(texts, text.Text)
		}
	}
	call.Text, call.IsError = strings.Join(texts, "\n"), res.IsError
	if res.StructuredContent != nil {
		if call.Result, err = json.Marshal(res.StructuredContent); err != nil {
			return call, err
		}
	}
	trace := cmp.Or(call.Text, noOutput)
	if call.IsError {
		trace = "error: " + trace
	}
	c.trace = append(c.trace, Turn{Role: "tool_result", Name: use.Name, Content: trace})
	return call, nil
}

func (c *Conversation) request(ctx context.Context) (*anthropic.Message, error) {
	limiterOnce.Do(func() { limiter = semaphore.NewWeighted(int64(max(c.cfg.Concurrency, 1))) })
	if err := limiter.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer limiter.Release(1)

	params := anthropic.MessageNewParams{
		Model:     c.cfg.Model,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: c.system, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
		Messages:  c.messages,
		Tools:     c.tools,
	}
	if c.cfg.Effort != "" {
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(c.cfg.Effort)}
	}
	return c.cfg.Client.Messages.New(ctx, params)
}

func (c *Conversation) record() {
	if c.cfg.Recorder == nil || len(c.prompts) == 0 {
		return
	}
	row := Row{
		PromptID:   c.t.Name(),
		Prompt:     strings.Join(c.prompts, "\n\n"),
		Tags:       []string{suite(c.t.Name())},
		Model:      c.model,
		StopReason: c.stop,
		Status:     c.status,
		Grade:      map[string]float64{"pass": grade(!c.t.Failed())},
		LatencyS:   c.latency.Seconds(),
		ToolCalls:  c.calls,
		Usage:      c.usage,
	}
	if c.refusal != "" {
		row.Meta = map[string]any{"refusal": c.refusal}
	}
	path, err := c.cfg.Recorder.Record(row, c.trace)
	if err != nil {
		c.t.Errorf("recording the transcript: %v", err)
		return
	}
	if c.t.Failed() {
		c.t.Logf("transcript: %s", path)
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

func (u *Usage) add(usage anthropic.Usage) {
	u.InputTokens += usage.InputTokens
	u.OutputTokens += usage.OutputTokens
	u.CacheReadInputTokens += usage.CacheReadInputTokens
	u.CacheCreationInputTokens += usage.CacheCreationInputTokens
}

func pretty(raw json.RawMessage) string {
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") != nil {
		return string(raw)
	}
	return buf.String()
}
