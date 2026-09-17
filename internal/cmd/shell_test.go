// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugin "unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/sandbox"

	"unikraft.com/x/shell"
	"unikraft.com/x/stdio"
)

func TestShellStatusIsTheCLIsExitStatus(t *testing.T) {
	assert.Equal(t, 3, ExitStatus(3).ExitCode())
	require.ErrorContains(t, ExitStatus(3), "status 3")

	status, ok := errors.AsType[ExitStatus](error(ExitStatus(3)))
	require.True(t, ok, "main unwraps the status this way, and exits with it rather than reporting an error")
	assert.Equal(t, 3, status.ExitCode())
}

func TestShellTimeoutFlags(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want sandboxTimeouts
	}{
		{"defaults", nil, defaultSandboxTimeouts},
		{"set", []string{"--interrupt-grace", "3s", "--reap-timeout", "7s"}, sandboxTimeouts{InterruptGrace: 3 * time.Second, Reap: 7 * time.Second}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var cli UnikraftCLI
			parser, err := NewParser(&cli)
			require.NoError(t, err)

			_, err = parser.Parse(append([]string{"instance", "shell", "my-inst"}, tt.args...))
			require.NoError(t, err)

			assert.Equal(t, tt.want.InterruptGrace, cli.Instances.Shell.InterruptGrace)
			assert.Equal(t, tt.want.Reap, cli.Instances.Shell.ReapTimeout)
		})
	}
}

func TestSandboxTransportFillsInDefaultTimeouts(t *testing.T) {
	assert.Equal(t, defaultSandboxTimeouts, newSandboxTransport(sandbox.Target{}, sandboxTimeouts{}).timeouts)

	partial := newSandboxTransport(sandbox.Target{}, sandboxTimeouts{Reap: time.Minute}).timeouts
	assert.Equal(t, sandboxTimeouts{InterruptGrace: defaultSandboxTimeouts.InterruptGrace, Reap: time.Minute}, partial,
		"a timeout left unset is the default, not zero: zero would wait forever")
}

func TestShellEnvFlagKeepsCommas(t *testing.T) {
	var cli UnikraftCLI
	parser, err := NewParser(&cli)
	require.NoError(t, err)

	_, err = parser.Parse([]string{"instance", "shell", "my-inst", "-e", "NO_PROXY=localhost,127.0.0.1", "-e", "DEBUG=true"})
	require.NoError(t, err)
	assert.Equal(t, []string{"NO_PROXY=localhost,127.0.0.1", "DEBUG=true"}, cli.Instances.Shell.Env,
		"a value with a comma is one record, as exec takes it")

	env, err := parseEnv(cli.Instances.Shell.Env)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"NO_PROXY": "localhost,127.0.0.1", "DEBUG": "true"}, env)

	_, err = parseEnv([]string{"NOVALUE"})
	require.ErrorContains(t, err, "--env", "a record without a value is refused, the same way exec refuses it")
}

// onePlugin runs a single command: it delivers out on standard output, exits
// with code, and records the signals it was sent.
type onePlugin struct {
	out  string
	code int32
	hang chan struct{}

	waiting sync.Once
	mu      sync.Mutex
	signals []int
}

func (f *onePlugin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/instances/inst-1/plugins/sandbox")

	data := map[string]any{"uuid": "cmd-1"}
	switch {
	case f.hang != nil && strings.HasSuffix(path, "/wait"):
		f.waiting.Do(func() { close(f.hang) })
		<-r.Context().Done()
		return

	case strings.HasSuffix(path, "/signal"):
		var req struct {
			Signal int `json:"signal"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		f.mu.Lock()
		f.signals = append(f.signals, req.Signal)
		f.mu.Unlock()

	case strings.HasSuffix(path, "/logs"):
		var req plugin.CommandLogsRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Stdout.Offset < uint64(len(f.out)) {
			data["stdout"] = base64.StdEncoding.EncodeToString([]byte(f.out[req.Stdout.Offset:]))
		}
		data["stdout_available"] = len(f.out)

	case path == "/commands/cmd-1" && r.Method == http.MethodGet:
		data["exitcode"] = f.code
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data})
}

func (f *onePlugin) sent() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.signals)
}

func fakeSandboxTransport(t *testing.T, fake http.Handler, timeouts sandboxTimeouts) *sandboxTransport {
	t.Helper()

	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	return newSandboxTransport(sandbox.Target{
		Client:   plugin.NewClient(),
		Instance: platform.Instance{Uuid: "inst-1"},
		Plugin:   "sandbox",
		Opts: []plugin.Option{
			plugin.WithEndpoint(srv.URL),
			plugin.WithPluginName("sandbox"),
			plugin.WithHTTPClient(srv.Client()),
		},
	}, timeouts)
}

// command is a line's worth of work for the transport, its output going nowhere.
func command(args ...string) shell.Command {
	return shell.Command{
		Args:    args,
		Dir:     "/",
		Streams: stdio.Stdio{Stdout: io.Discard, Stderr: io.Discard},
	}
}

func TestAFailedCommandIsNotAFailedCLI(t *testing.T) {
	transport := fakeSandboxTransport(t, &onePlugin{code: 3}, sandboxTimeouts{})

	code, err := transport.Exec(t.Context(), command("false"))

	require.NoError(t, err, "sending the command forward is what the CLI was asked to do")
	assert.Equal(t, 3, code, "the status is the shell's to put in $?, not the CLI's to exit with")
}

// deafWriter is the far end of a pipeline whose reader has gone.
type deafWriter struct{}

func (deafWriter) Write([]byte) (int, error) { return 0, syscall.EPIPE }

func TestAnUninterruptibleCommandGivesThePromptBack(t *testing.T) {
	fake := &onePlugin{hang: make(chan struct{})}
	grace := 500 * time.Millisecond
	transport := fakeSandboxTransport(t, fake, sandboxTimeouts{InterruptGrace: grace})

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = transport.Exec(ctx, command("stubborn"))
	}()

	<-fake.hang
	cancel()

	select {
	case <-done:
	case <-time.After(grace + 20*time.Second):
		t.Fatal("a command that ignores the signal held the prompt forever")
	}
}

func TestAClosedPipeReachesTheInstance(t *testing.T) {
	fake := &onePlugin{out: "output nobody reads\n", code: 3}
	transport := fakeSandboxTransport(t, fake, sandboxTimeouts{})

	cmd := command("yes")
	cmd.Streams.Stdout = deafWriter{}
	code, err := transport.Exec(t.Context(), cmd)

	require.NoError(t, err)
	assert.Equal(t, 3, code, "the command still ran to the end, so its status is the one to report")
	assert.Equal(t, []int{int(syscall.SIGPIPE)}, fake.sent(),
		"the instance is told the reader has gone, the way a local pipeline tells it")
}

func TestAFailedCommandLineIsTheCLIsStatus(t *testing.T) {
	transport := fakeSandboxTransport(t, &onePlugin{code: 3}, sandboxTimeouts{})

	code, err := shell.Run(t.Context(), shell.Config{
		Instance:  "inst-1",
		Dir:       "/",
		Command:   "/bin/nonsense",
		Transport: transport,
	}, stdio.Stdio{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard})

	require.NoError(t, err, "a command that failed on the instance is not the CLI failing")
	assert.Equal(t, 3, code, "but what a -c line ended on is what the CLI exits with")
}

// TestExecSurvivesAClosedPipe pins that a reader which stops early — "exec ... |
// head" — leaves the command's own status the one reported, and tells the
// instance the way a local pipeline would, rather than failing the drain and
// reporting a broken pipe instead of what the command did.
func TestExecSurvivesAClosedPipe(t *testing.T) {
	fake := &onePlugin{out: "output nobody reads\n", code: 3}
	target := fakeSandboxTransport(t, fake, sandboxTimeouts{}).target

	err := (&ExecSandboxInstanceCmd{Cmd: []string{"yes"}}).runOn(t.Context(), target,
		config.Stdio{Stdout: deafWriter{}, Stderr: io.Discard})

	var status ExitStatus
	require.ErrorAs(t, err, &status, "the command still ran to the end")
	assert.Equal(t, 3, status.ExitCode())
	assert.Equal(t, []int{int(syscall.SIGPIPE)}, fake.sent(),
		"the instance is told the reader has gone, the way a local pipeline tells it")
}
