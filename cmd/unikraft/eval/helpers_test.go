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
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/eval"
	integ "unikraft.com/cli/internal/integration"
	"unikraft.com/cli/internal/mcpserver/mcptest"
)

const (
	staging   = "staging"
	liveImage = "nginx:latest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	integ.Cleanup()
	os.Exit(code)
}

var (
	configOnce sync.Once
	config     eval.Config
	outDir     string
	configErr  error
)

// loadConfig reads the run's settings from UNIKRAFT_EVAL_MODEL, _EFFORT,
// _CONCURRENCY, _TIMEOUT and _OUT, the last a temporary directory by
// default.
func loadConfig() (eval.Config, string, error) {
	cfg := eval.Config{
		Client:   anthropic.NewClient(option.WithMaxRetries(5), option.WithRequestTimeout(2*time.Minute)),
		Model:    cmp.Or(os.Getenv("UNIKRAFT_EVAL_MODEL"), "claude-sonnet-5-5"),
		Effort:   os.Getenv("UNIKRAFT_EVAL_EFFORT"),
		MaxTurns: 12,
	}
	var err error
	if cfg.Concurrency, err = envInt("UNIKRAFT_EVAL_CONCURRENCY", 4); err != nil {
		return cfg, "", err
	}
	if cfg.Timeout, err = envDuration("UNIKRAFT_EVAL_TIMEOUT", 3*time.Minute); err != nil {
		return cfg, "", err
	}
	dir := os.Getenv("UNIKRAFT_EVAL_OUT")
	if dir == "" {
		if dir, err = os.MkdirTemp("", "unikraft-eval-*"); err != nil {
			return cfg, "", err
		}
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return cfg, "", err
	}
	cfg.Recorder = eval.NewRecorder(dir)
	return cfg, dir, nil
}

func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, value)
	}
	return n, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration, got %q", name, value)
	}
	return d, nil
}

func system(instructions, metro string) string {
	return fmt.Sprintf(`You operate Unikraft Cloud for the user through these tools. Act directly and do not ask clarifying questions. Names the user gives are exact resource keys. Deploy to the metro %s unless the user names another.

%s`, metro, instructions)
}

// live gives a conversation with a model over the tools of the built CLI's
// MCP server, which acts on the staging platform with the test's own config
// and partition, and the environment to set up and inspect resources with
// the CLI.
func live(t *testing.T) (*integ.TestEnv, *eval.Conversation) {
	t.Helper()
	eval.SkipUnlessEvals(t)
	integ.RequireSupportedServer(t, []string{staging})
	t.Parallel()
	configOnce.Do(func() { config, outDir, configErr = loadConfig() })
	require.NoError(t, configErr)
	t.Logf("eval results: %s", outDir)

	env := integ.NewEnv(t, true, cmd.PartitionedResources...)
	c := env.Command(context.WithoutCancel(t.Context()), []string{"unikraft", "mcp", "--docs-url="})
	var stderr bytes.Buffer
	c.Stderr = &stderr
	t.Cleanup(func() {
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("mcp server output:\n%s", stderr.String())
		}
	})
	session := mcptest.Connect(t, &mcp.CommandTransport{Command: c})
	return env, eval.New(t, session.ClientSession, config, system(session.Instructions, env.MetroName))
}

// newInstance creates an instance from a public image with the CLI and gives
// its name.
func newInstance(t *testing.T, env *integ.TestEnv, args ...string) string {
	t.Helper()
	name := "test-" + integ.Uniq()
	env.Run(t, append([]string{
		"unikraft", "instance", "create",
		"--output", "quiet",
		"--name", name,
		"--metro", env.MetroName,
		"--image", liveImage,
		"--memory", "256",
	}, args...))
	return name
}

func waitState(t *testing.T, env *integ.TestEnv, name, state string) {
	t.Helper()
	env.Run(t, []string{"unikraft", "--timeout", "60s", "instance", "wait", "--until", "state==" + state, name})
}

// only gives the single call of the tool name in the reply.
func only(t *testing.T, reply eval.Reply, name string) eval.Call {
	t.Helper()
	calls := reply.Called(name)
	require.Len(t, calls, 1, "calls of %s in %v", name, reply.Tools())
	return calls[0]
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(raw, &v), "decoding %s", raw)
	return v
}

// bare drops the metro/ prefix a model may put on resource keys.
func bare(keys ...string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		_, name, found := strings.Cut(key, "/")
		if !found {
			name = key
		}
		out = append(out, name)
	}
	return out
}

// text gives a set value as the CLI would read it: strings unquoted, other
// JSON values as written.
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

type keysArgs struct {
	Keys   []string `json:"keys"`
	Filter []string `json:"filter"`
}

type controlArgs struct {
	Action string   `json:"action"`
	Keys   []string `json:"keys"`
}

type patchArgs struct {
	Key    string                     `json:"key"`
	Set    map[string]json.RawMessage `json:"set"`
	DryRun bool                       `json:"dry_run"`
}

type listResult struct {
	Resources []struct {
		Name string `json:"name"`
	} `json:"resources"`
}
