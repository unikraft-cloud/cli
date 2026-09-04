// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
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

	"github.com/charmbracelet/x/ansi"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugin "unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/sandbox"
	"unikraft.com/cli/internal/shell"
)

func TestShellBuiltinNames(t *testing.T) {
	names := shellBuiltins{}.Names()

	assert.Equal(t, []string{
		"edit", "get", "help", "mount", "restart", "start", "stop", "suspend",
		"unmount", "volumes",
	}, names, "sorted, so completion and help are stable")

	for _, node := range shellBuiltinNodes() {
		assert.NotEmpty(t, node.Help, "%s needs help text", node.Name)
		assert.NotNil(t, node.Target, "%s needs something to run", node.Name)
	}
}

func TestShellBuiltinsReportWhatIsWrongWithALine(t *testing.T) {
	for _, tt := range []struct {
		name string
		line []string
		want string
		code int
	}{
		// An unknown builtin is not the builtin's failure, so it reports no
		// status of its own and the shell answers with one.
		{"unknown-builtin", []string{"nonsense"}, `unknown builtin "nonsense"; try ":help"`, 0},
		{"missing-arguments", []string{"mount"}, "<volume>", 1},
		{"missing-one-argument", []string{"mount", "vol"}, "<path>", 1},
		{"unknown-flag", []string{"get", "--nonsense"}, "unknown flag --nonsense", 1},
		{"not-a-field", []string{"edit", "nonsense"}, "is not <field>=<value>", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer

			code, err := shellBuiltins{}.Run(t.Context(),
				shell.Streams{Out: &out, Err: &out}, tt.line)

			require.Error(t, err)
			assert.Equal(t, tt.code, code)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestShellBuiltinAnswersItsOwnHelp(t *testing.T) {
	var out bytes.Buffer

	code, err := shellBuiltins{}.Run(t.Context(),
		shell.Streams{Out: &out, Err: &out}, []string{"mount", "--help"})

	require.NoError(t, err)
	assert.Zero(t, code)
	assert.Contains(t, out.String(), "<volume>")
	assert.Contains(t, out.String(), "--readonly")
}

func TestShellBuiltinHelp(t *testing.T) {
	var out bytes.Buffer
	code, err := shellBuiltins{}.Run(t.Context(), shell.Streams{Out: &out}, []string{"help"})

	require.NoError(t, err)
	assert.Zero(t, code)

	printed := out.String()
	assert.Contains(t, printed, ":mount <volume> <path>")
	assert.Contains(t, printed, ":unmount <volume>")
	assert.Contains(t, printed, ":edit <field=value>")
	assert.NotContains(t, printed, "runs on the instance")
	assert.Contains(t, printed, "Detach a volume from this instance.")
	for _, name := range (shellBuiltins{}).Names() {
		assert.Contains(t, printed, ":"+name)
	}
}

func TestShellVolumeBuiltinsReportOnlyTheHint(t *testing.T) {
	stdio := quiet(config.Stdio{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	assert.Equal(t, io.Discard, stdio.Stdout, "the resource diff has nowhere to go")

	hint := restartHint("the instance mounts volumes at boot")
	assert.Contains(t, hint, `":restart" for this to take effect`)
	assert.NotEqual(t, ansi.Strip(hint), hint,
		"the heads-up is coloured apart from the shell's own hints")
}

func TestShellBuiltinsThatRestart(t *testing.T) {
	b := shellBuiltins{}

	for _, name := range []string{"restart", "start"} {
		assert.True(t, b.Restarts([]string{name}), "%s brings the instance back", name)
	}
	for _, name := range []string{"edit", "get", "help", "mount", "stop", "suspend", "unmount", "volumes"} {
		assert.False(t, b.Restarts([]string{name}), "%s leaves the shell nothing to wait for", name)
	}
	assert.False(t, b.Restarts(nil))

	var _ shell.Restarts = b
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

func newSandboxTransport(t *testing.T, fake http.Handler) sandboxTransport {
	t.Helper()

	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	return sandboxTransport{target: sandbox.Target{
		Client:   plugin.NewClient(),
		Instance: platform.Instance{Uuid: "inst-1"},
		Plugin:   "sandbox",
		Opts: []plugin.Option{
			plugin.WithEndpoint(srv.URL),
			plugin.WithPluginName("sandbox"),
			plugin.WithHTTPClient(srv.Client()),
		},
	}}
}

func TestAFailedCommandIsNotAFailedCLI(t *testing.T) {
	transport := newSandboxTransport(t, &onePlugin{code: 3})

	code, err := transport.Exec(t.Context(),
		shell.Streams{Out: io.Discard, Err: io.Discard}, "/", nil, []string{"false"})

	require.NoError(t, err, "sending the command forward is what the CLI was asked to do")
	assert.Equal(t, 3, code, "the status is the shell's to put in $?, not the CLI's to exit with")
}

// deafWriter is the far end of a pipeline whose reader has gone.
type deafWriter struct{}

func (deafWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestAnUninterruptibleCommandGivesThePromptBack(t *testing.T) {
	fake := &onePlugin{hang: make(chan struct{})}
	transport := newSandboxTransport(t, fake)

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = transport.Exec(ctx, shell.Streams{Out: io.Discard, Err: io.Discard}, "/", nil, []string{"stubborn"})
	}()

	<-fake.hang
	cancel()

	select {
	case <-done:
	case <-time.After(sandboxInterruptGrace + 20*time.Second):
		t.Fatal("a command that ignores the signal held the prompt forever")
	}
}

func TestAClosedPipeReachesTheInstance(t *testing.T) {
	fake := &onePlugin{out: "output nobody reads\n", code: 3}
	transport := newSandboxTransport(t, fake)

	code, err := transport.Exec(t.Context(),
		shell.Streams{Out: deafWriter{}, Err: io.Discard}, "/", nil, []string{"yes"})

	require.NoError(t, err)
	assert.Equal(t, 3, code, "the command still ran to the end, so its status is the one to report")
	assert.Equal(t, []int{int(syscall.SIGPIPE)}, fake.sent(),
		"the instance is told the reader has gone, the way a local pipeline tells it")
}

func TestShellHelpMentionsBuiltins(t *testing.T) {
	assert.Contains(t, ShellSandboxInstanceCmd{}.Help(), ":help")
}
