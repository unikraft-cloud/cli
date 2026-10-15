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
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"golang.org/x/sync/semaphore"
)

// Statuses of a run.
const (
	StatusOK        = "ok"
	StatusTruncated = "truncated"
	StatusRefusal   = "refusal"
	StatusMaxTurns  = "max_turns"
)

const (
	maxTokens = 4096
	noOutput  = "(no output)"
)

// ErrStop ends a run when an executor returns it for a call: the call is
// recorded as the last step, unexecuted, and the run has StatusOK.
var ErrStop = errors.New("stop the run")

// Agent drives a model through a tool loop over the tools of an MCP server.
type Agent struct {
	Client anthropic.Client
	Config Config
	// System is the system prompt, normally a preamble followed by the
	// server's instructions.
	System string
	Tools  []anthropic.ToolUnionParam
	// Exec runs the tool calls. When nil, a run stops at the first turn that
	// calls a tool, without executing it.
	Exec Exec
}

// Exec runs a tool call and gives its text result and whether the tool
// reported an error, or ErrStop to end the run at that call.
type Exec func(ctx context.Context, name string, args json.RawMessage) (string, bool, error)

// Step is one tool call of a run.
type Step struct {
	Tool    string
	Args    json.RawMessage
	Result  string
	IsError bool

	id string
}

// Result is the outcome of one run of a prompt.
type Result struct {
	// Steps are the tool calls in order. Without an executor they are the
	// calls of the first turn that made any, unexecuted.
	Steps []Step
	// Text is the assistant text of the last turn.
	Text       string
	StopReason string
	Status     string
	// Refusal is the category of a refusal, when Status is StatusRefusal.
	Refusal string
	// Model is the model that served the run.
	Model   string
	Usage   Usage
	Latency time.Duration
	Trace   []Turn
}

var (
	limiterOnce sync.Once
	limiter     *semaphore.Weighted
)

// Run sends the prompts in turn in one conversation, each once the model
// has answered the previous one.
func (a *Agent) Run(ctx context.Context, prompts ...string) (*Result, error) {
	if len(prompts) == 0 {
		return nil, errors.New("no prompt")
	}
	start := time.Now()
	res := &Result{Trace: []Turn{{Role: "system", Content: a.System}}}
	if err := a.run(ctx, prompts, res); err != nil {
		return nil, err
	}
	res.Latency = time.Since(start)
	return res, nil
}

func (a *Agent) run(ctx context.Context, prompts []string, res *Result) error {
	var messages []anthropic.MessageParam
	send := func() {
		res.Trace = append(res.Trace, Turn{Role: "user", Content: prompts[0]})
		messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(prompts[0])))
		prompts = prompts[1:]
	}
	send()
	for range a.Config.MaxTurns {
		msg, err := a.send(ctx, messages)
		if err != nil {
			return err
		}
		messages = append(messages, msg.ToParam())
		res.Model = msg.Model
		res.StopReason = string(msg.StopReason)
		res.Usage.add(msg.Usage)

		var texts []string
		first := len(res.Steps)
		for _, block := range msg.Content {
			switch v := block.AsAny().(type) {
			case anthropic.TextBlock:
				texts = append(texts, v.Text)
				res.Trace = append(res.Trace, Turn{Role: "assistant", Content: v.Text})
			case anthropic.ToolUseBlock:
				res.Steps = append(res.Steps, Step{Tool: v.Name, Args: json.RawMessage(v.Input), id: v.ID})
				res.Trace = append(res.Trace, Turn{Role: "tool_call", Name: v.Name, Content: pretty(v.Input)})
			}
		}
		res.Text = strings.Join(texts, "\n")

		switch msg.StopReason {
		case anthropic.StopReasonRefusal:
			res.Status = StatusRefusal
			res.Refusal = string(msg.StopDetails.Category)
			return nil
		case anthropic.StopReasonMaxTokens:
			res.Status = StatusTruncated
			return nil
		case anthropic.StopReasonToolUse:
			if a.Exec == nil {
				res.Status = StatusOK
				return nil
			}
			calls := res.Steps[first:]
			results := make([]anthropic.ContentBlockParamUnion, 0, len(calls))
			for i := range calls {
				step := &calls[i]
				text, isError, err := a.Exec(ctx, step.Tool, step.Args)
				if errors.Is(err, ErrStop) {
					res.Steps = res.Steps[:first+i+1]
					res.Status = StatusOK
					return nil
				}
				if err != nil {
					return err
				}
				step.Result, step.IsError = text, isError
				content := cmp.Or(text, noOutput)
				trace := content
				if isError {
					trace = "error: " + content
				}
				res.Trace = append(res.Trace, Turn{Role: "tool_result", Name: step.Tool, Content: trace})
				results = append(results, anthropic.NewToolResultBlock(step.id, content, isError))
			}
			messages = append(messages, anthropic.NewUserMessage(results...))
		default:
			if len(prompts) == 0 {
				res.Status = StatusOK
				return nil
			}
			send()
		}
	}
	res.Status = StatusMaxTurns
	return nil
}

func (a *Agent) send(ctx context.Context, messages []anthropic.MessageParam) (*anthropic.Message, error) {
	limiterOnce.Do(func() { limiter = semaphore.NewWeighted(int64(max(a.Config.Concurrency, 1))) })
	if err := limiter.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer limiter.Release(1)

	params := anthropic.MessageNewParams{
		Model:     a.Config.Model,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: a.System, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
		Messages:  messages,
		Tools:     a.Tools,
	}
	if a.Config.Effort != "" {
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(a.Config.Effort)}
	}
	return a.Client.Messages.New(ctx, params)
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
