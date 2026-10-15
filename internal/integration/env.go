// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
)

// TestEnv holds per-test environment for running CLI commands deterministically.
type TestEnv struct {
	// Config is the integration test config, or nil if not loaded (offline tests).
	Config *Config

	unikraftPath  string
	configPath    string
	partitionPath string
}

func NewTestEnv(t *testing.T, unikraftPath string) *TestEnv {
	t.Helper()
	dir := t.TempDir()
	return &TestEnv{
		unikraftPath:  unikraftPath,
		configPath:    filepath.Join(dir, "config.yaml"),
		partitionPath: filepath.Join(dir, "partition.json"),
	}
}

// NewEnv builds the CLI and gives an environment with its own copy of the
// test config, when one is available, and a partition whose resources are
// torn down when the test ends. With online set, the test is skipped when
// there is no config.
func NewEnv(t *testing.T, online bool, teardown ...resource.Resource) *TestEnv {
	t.Helper()
	env := NewTestEnv(t, BuildUnikraft(t))
	cfg, err := LoadConfig(t)
	require.NoError(t, err)
	if cfg == nil && online {
		t.Skip("online test requires config, but no config found")
	}
	if cfg != nil {
		cfg.Config.Path = env.configPath
		require.NoError(t, cfg.Config.Save())
		env.Config = cfg
	}

	ctx := log.WithLogger(t.Context(), log.New(t.Output(), log.TextType, log.TraceLevel))
	t.Cleanup(func() {
		if cfg != nil {
			ctx = config.WithConfig(ctx, cfg.Config)
		}
		if _, err := os.Stat(env.partitionPath); os.IsNotExist(err) {
			return
		}
		partition, err := resource.LoadPartition(env.partitionPath, teardown...)
		require.NoError(t, err)
		require.NotNil(t, partition)
		require.NoError(t, partition.Teardown(context.WithoutCancel(ctx)))
	})
	return env
}

var (
	uniqSeed    = strings.ToLower(rand.Text()[:6])
	uniqCounter atomic.Uint64
)

// Uniq gives a name suffix that is unique across the test binary.
func Uniq() string {
	return fmt.Sprintf("%s%06x", uniqSeed, uniqCounter.Add(1))
}

func (env *TestEnv) WithConfig(cfg *Config, configPath string) *TestEnv {
	env.Config = cfg
	env.configPath = configPath
	return env
}

func (env *TestEnv) WithPartitionPath(partitionPath string) *TestEnv {
	env.partitionPath = partitionPath
	return env
}

type CmdOption func(*cmdConfig)

type cmdConfig struct {
	workDir       string
	stdin         string
	expectFail    bool
	allowFail     bool
	noPartition   bool
	timeout       time.Duration
	withoutCancel bool
	exitCode      *int
}

func WithWorkDir(dir string) CmdOption {
	return func(c *cmdConfig) { c.workDir = dir }
}

// WithStdin feeds in to the command's standard input, which is otherwise empty.
func WithStdin(in string) CmdOption {
	return func(c *cmdConfig) { c.stdin = in }
}

func ExpectFail() CmdOption {
	return func(c *cmdConfig) { c.expectFail = true }
}

// ExpectExitCode requires the command to exit with exactly code.
func ExpectExitCode(code int) CmdOption {
	return func(c *cmdConfig) { c.exitCode = &code }
}

func AllowFail() CmdOption {
	return func(c *cmdConfig) { c.allowFail = true }
}

func WithoutCancel() CmdOption {
	return func(c *cmdConfig) { c.withoutCancel = true }
}

// WithTimeout kills the command after d. The caller is responsible for
// handling the resulting error (e.g. by also passing AllowFail).
// Use for commands that run indefinitely (e.g. --follow).
func WithTimeout(d time.Duration) CmdOption {
	return func(c *cmdConfig) { c.timeout = d }
}

// WithNoPartition disables partition tracking for the command. The resource is not
// registered in a test partition and stays after individual test cleanup.
func WithNoPartition() CmdOption {
	return func(c *cmdConfig) { c.noPartition = true }
}

// Command gives an unstarted command for args with the environment that Run
// and StartBackground give their children, and no stdout or stderr. The
// caller owns the process.
func (env *TestEnv) Command(ctx context.Context, args []string, opts ...CmdOption) *exec.Cmd {
	var cfg cmdConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return env.command(ctx, args, cfg)
}

func (env *TestEnv) command(ctx context.Context, args []string, cfg cmdConfig) *exec.Cmd {
	var c *exec.Cmd
	if args[0] == "unikraft" {
		c = exec.CommandContext(ctx, env.unikraftPath, args[1:]...)
	} else {
		c = exec.CommandContext(ctx, args[0], args[1:]...)
	}
	c.Dir = cfg.workDir
	if cfg.stdin != "" {
		c.Stdin = strings.NewReader(cfg.stdin)
	}
	c.Env = slices.DeleteFunc(os.Environ(), func(s string) bool {
		return strings.HasPrefix(s, "UNIKRAFT_")
	})
	c.Env = append(c.Env, "NO_COLOR=1", "UNIKRAFT_CONFIG="+env.configPath, "BUILDKIT_PROGRESS=quiet")
	if !cfg.noPartition {
		c.Env = append(c.Env, resource.UnikraftPartitionEnv+"="+env.partitionPath)
	}
	return c
}

func (env *TestEnv) RunRaw(t *testing.T, args []string, opts ...CmdOption) (string, error) {
	t.Helper()
	var cfg cmdConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	t.Logf("executing: %s", strings.Join(args, " "))

	cmdCtx := t.Context()
	if cfg.withoutCancel {
		cmdCtx = context.WithoutCancel(cmdCtx)
	}
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		cmdCtx, cancel = context.WithTimeout(cmdCtx, cfg.timeout)
		defer cancel()
	}

	c := env.command(cmdCtx, args, cfg)
	var output bytes.Buffer
	c.Stdout = &output
	c.Stderr = &output

	err := c.Run()
	return ansi.Strip(output.String()), err
}

func (env *TestEnv) Run(t *testing.T, args []string, opts ...CmdOption) string {
	t.Helper()
	var cfg cmdConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	out, err := env.RunRaw(t, args, opts...)
	switch {
	case cfg.exitCode != nil:
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "command %q was expected to exit with %d\n%s", strings.Join(args, " "), *cfg.exitCode, out)
		require.Equal(t, *cfg.exitCode, exitErr.ExitCode(), "command %q exited with the wrong status\n%s", strings.Join(args, " "), out)
	case cfg.expectFail:
		require.Error(t, err, "command %q was expected to fail but succeeded\n%s", strings.Join(args, " "), out)
	case cfg.allowFail:
		// ignore error
	default:
		require.NoError(t, err, "command %q failed\n%s", strings.Join(args, " "), out)
	}
	return out
}

// BackgroundProcess is a handle to a process started by StartBackground.
type BackgroundProcess struct {
	cmd    *exec.Cmd
	output *bytes.Buffer
	t      *testing.T
	once   sync.Once
	err    error
}

// Stop terminates the background process, waiting for it to exit. It is safe
// to call multiple times, and is registered automatically as test cleanup by
// StartBackground, so it only needs to be called explicitly for early
// termination.
func (p *BackgroundProcess) Stop() {
	p.t.Helper()
	p.t.Logf("stopping background process")
	_ = p.cmd.Cancel()
	if err := p.wait(); err != nil && p.output.Len() > 0 {
		p.t.Logf("background process output:\n%s", p.output.String())
	}
}

func (p *BackgroundProcess) Interrupt() {
	p.t.Helper()
	p.t.Logf("interrupting background process")
	require.NoError(p.t, p.cmd.Process.Signal(os.Interrupt))
}

func (p *BackgroundProcess) Wait() (string, error) {
	p.t.Helper()
	err := p.wait()
	return ansi.Strip(p.output.String()), err
}

func (p *BackgroundProcess) wait() error {
	p.once.Do(func() { p.err = p.cmd.Wait() })
	return p.err
}

// StartBackground starts a command in the background and registers cleanup to
// stop it when the test ends. If waitAddr is non-empty, it polls that TCP
// address until it is reachable or the timeout (default 90s) expires.
// The returned handle's Stop method can be called for early termination.
func (env *TestEnv) StartBackground(t *testing.T, args []string, waitAddr string, timeout time.Duration) *BackgroundProcess {
	t.Helper()
	t.Logf("starting background: %s", strings.Join(args, " "))

	c := env.command(t.Context(), args, cmdConfig{})
	c.Cancel = func() error {
		return c.Process.Signal(os.Interrupt)
	}
	c.WaitDelay = 30 * time.Second

	var output bytes.Buffer
	c.Stdout = &output
	c.Stderr = &output

	require.NoError(t, c.Start(), "failed to start background command %q", strings.Join(args, " "))

	proc := &BackgroundProcess{cmd: c, output: &output, t: t}
	t.Cleanup(proc.Stop)

	if waitAddr != "" {
		waitForAddr(t, waitAddr, timeout)
	}

	return proc
}

func waitForAddr(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to become reachable", addr)
}
