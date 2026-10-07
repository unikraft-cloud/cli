// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"bytes"
	"context"
	"fmt"
	"os"
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

const staging = "staging"

func TestMain(m *testing.M) {
	code := m.Run()
	integ.Cleanup()
	os.Exit(code)
}

func system(instructions, metro string) string {
	return fmt.Sprintf(`You operate Unikraft Cloud for the user through these tools. Act directly and do not ask clarifying questions. Names the user gives are exact resource keys. Deploy to the metro %s unless the user names another.

%s`, metro, instructions)
}

var (
	setupOnce   sync.Once
	setupConfig eval.Config
	setupClient anthropic.Client
	setupErr    error
)

func setup(t *testing.T) (eval.Config, anthropic.Client) {
	t.Helper()
	eval.SkipUnlessEval(t)
	setupOnce.Do(func() {
		setupConfig, setupErr = eval.LoadConfig()
		setupClient = anthropic.NewClient(option.WithMaxRetries(5), option.WithRequestTimeout(2*time.Minute))
	})
	require.NoError(t, setupErr)
	t.Logf("eval results: %s", setupConfig.OutDir)
	return setupConfig, setupClient
}

// mcpServer starts the built CLI's MCP server for env, to be closed through
// the session that connects to it.
func mcpServer(t *testing.T, env *integ.TestEnv) mcp.Transport {
	t.Helper()
	c := env.Command(context.WithoutCancel(t.Context()), []string{"unikraft", "mcp", "--docs-url="})
	var stderr bytes.Buffer
	c.Stderr = &stderr
	t.Cleanup(func() {
		if t.Failed() && stderr.Len() > 0 {
			t.Logf("mcp server output:\n%s", stderr.String())
		}
	})
	return &mcp.CommandTransport{Command: c}
}

func newRunner(t *testing.T, cfg eval.Config, client anthropic.Client, env *integ.TestEnv, metro string, exec func(*mcptest.Session) eval.Exec) *eval.Runner {
	t.Helper()
	session := mcptest.Connect(t, mcpServer(t, env))
	tools, err := eval.ToolParams(session.Tools)
	require.NoError(t, err)
	return eval.NewRunner(&eval.Agent{
		Client: client,
		Config: cfg,
		System: system(session.Instructions, metro),
		Tools:  tools,
		Exec:   exec(session),
	}, cfg)
}

// selection gives a runner whose agent sees the tools of the MCP server,
// gets canned answers to the read-only ones and stops at the first call
// that would change something, so that a run ends at that call.
func selection(t *testing.T) *eval.Runner {
	t.Helper()
	cfg, client := setup(t)
	return newRunner(t, cfg, client, integ.NewTestEnv(t, integ.BuildUnikraft(t)), "fra", stub)
}

// live gives a runner whose agent executes the tools against the staging
// platform, with the test's own config and partition, and the environment
// to inspect the outcome with the CLI.
func live(t *testing.T) (*eval.Runner, *integ.TestEnv) {
	t.Helper()
	cfg, client := setup(t)
	integ.SkipUnlessSupportedServer(t, []string{staging})
	t.Parallel()

	env := integ.NewEnv(t, true, cmd.PartitionedResources...)
	cfg.Reps = 1
	return newRunner(t, cfg, client, env, env.Config.MetroName, func(s *mcptest.Session) eval.Exec { return s.Call }), env
}
