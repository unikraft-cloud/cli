// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugin "unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"
)

// mockPlugin is a sandbox plugin that serves one command: enough of the API
// for a Cmd to run against, and a record of what it was asked.
type mockPlugin struct {
	mu sync.Mutex

	stdout, stderr []byte

	exited   chan struct{}
	exitCode int32

	run      plugin.RunCommandRequest
	stdin    []byte
	stdinEOF bool

	signals []int
}

func newFakePlugin() *mockPlugin {
	return &mockPlugin{exited: make(chan struct{})}
}

func (f *mockPlugin) write(stdout, stderr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stdout = append(f.stdout, stdout...)
	f.stderr = append(f.stderr, stderr...)
}

func (f *mockPlugin) exit(code int32) {
	f.mu.Lock()
	f.exitCode = code
	f.mu.Unlock()
	close(f.exited)
}

func (f *mockPlugin) sentSignals() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.signals...)
}

func (f *mockPlugin) stdinSeen() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.stdin), f.stdinEOF
}

func (f *mockPlugin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const prefix = "/v1/instances/inst-1/plugins/sandbox"
	path := strings.TrimPrefix(r.URL.Path, prefix)

	reply := func(data any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data})
	}

	switch {
	case path == "/commands" && r.Method == http.MethodPost:
		f.mu.Lock()
		_ = json.NewDecoder(r.Body).Decode(&f.run)
		f.mu.Unlock()
		reply(plugin.RunCommandData{Uuid: "cmd-1"})

	case path == "/commands/cmd-1/wait":
		select {
		case <-f.exited:
		case <-r.Context().Done():
			// The client gave up; answering is pointless.
			return
		}
		reply(nil)

	case path == "/commands/cmd-1" && r.Method == http.MethodGet:
		f.mu.Lock()
		code := f.exitCode
		f.mu.Unlock()
		reply(plugin.GetCommandData{Uuid: "cmd-1", Exitcode: code})

	case path == "/commands/cmd-1/logs":
		var req plugin.CommandLogsRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		f.mu.Lock()
		out, outAvail := slice(f.stdout, req.Stdout.Offset)
		errOut, errAvail := slice(f.stderr, req.Stderr.Offset)
		f.mu.Unlock()

		reply(plugin.CommandLogsData{
			Stdout:          base64.StdEncoding.EncodeToString(out),
			Stderr:          base64.StdEncoding.EncodeToString(errOut),
			StdoutAvailable: outAvail,
			StderrAvailable: errAvail,
		})

	case path == "/commands/cmd-1/stdin" && r.Method == http.MethodPost:
		var req plugin.CommandStdinRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		decoded, _ := base64.StdEncoding.DecodeString(req.Data)

		f.mu.Lock()
		f.stdin = append(f.stdin, decoded...)
		if req.Eof != nil && *req.Eof {
			f.stdinEOF = true
		}
		f.mu.Unlock()
		reply(nil)

	case path == "/commands/cmd-1/signal" && r.Method == http.MethodPost:
		var req struct {
			Signal int `json:"signal"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		f.mu.Lock()
		f.signals = append(f.signals, req.Signal)
		f.mu.Unlock()
		reply(nil)

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// slice returns what follows offset, and how much there is in total.
func slice(b []byte, offset uint64) ([]byte, uint64) {
	if offset > uint64(len(b)) {
		return nil, uint64(len(b))
	}
	return b[offset:], uint64(len(b))
}

// newTarget serves fake over a test server and returns a Target addressing
// it.
func newTarget(t *testing.T, fake *mockPlugin) Target {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	return Target{
		Client:   plugin.NewClient(),
		Instance: platform.Instance{Uuid: "inst-1"},
		Plugin:   "sandbox",
		Opts: []plugin.Option{
			plugin.WithEndpoint(srv.URL),
			plugin.WithPluginName("sandbox"),
			plugin.WithToken("token"),
			plugin.WithHTTPClient(srv.Client()),
		},
	}
}

// TestCmdRun pins the whole of a successful run: the command line the plugin
// is asked to run and the output both streams deliver.
func TestCmdRun(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	fake.write("hello\n", "warning\n")
	fake.exit(0)

	var stdout, stderr bytes.Buffer
	cmd := target.Command(t.Context(), "echo", "hello")
	cmd.Dir = "/srv"
	cmd.Env = map[string]string{"DEBUG": "true"}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run())

	assert.Equal(t, "hello\n", stdout.String())
	assert.Equal(t, "warning\n", stderr.String())
	assert.Equal(t, "cmd-1", cmd.UUID)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Equal(t, `'echo' 'hello'`, fake.run.Cmd)
	require.NotNil(t, fake.run.Cwd)
	assert.Equal(t, "/srv", *fake.run.Cwd)
	require.NotNil(t, fake.run.Env)
	assert.Equal(t, map[string]string{"DEBUG": "true"}, *fake.run.Env)
}

// TestCmdStreamsStayApart pins that the two streams are never folded into
// one: a nil writer discards its stream rather than sending it to the other.
func TestCmdStreamsStayApart(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	fake.write("out", "err")
	fake.exit(0)

	var stdout, stderr bytes.Buffer
	cmd := target.Command(t.Context(), "sh", "-c", "...")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run())
	assert.Equal(t, "out", stdout.String())
	assert.Equal(t, "err", stderr.String())
}

// TestCmdNilStderrDiscards pins that a caller that set only Stdout sees only
// the command's standard output, as os/exec does.
func TestCmdNilStderrDiscards(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	fake.write("out", "err")
	fake.exit(0)

	var stdout bytes.Buffer
	cmd := target.Command(t.Context(), "sh", "-c", "...")
	cmd.Stdout = &stdout

	require.NoError(t, cmd.Run())
	assert.Equal(t, "out", stdout.String())
}

// TestCmdStdin pins that a reader is forwarded in full and that the command's
// standard input is closed after it.
func TestCmdStdin(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	cmd := target.Command(t.Context(), "cat")
	cmd.Stdin = strings.NewReader("one\ntwo\n")
	cmd.Stdout = io.Discard

	require.NoError(t, cmd.Start())

	// The command only ends once its input has been read to the end, as cat
	// does.
	require.Eventually(t, func() bool {
		_, eof := fake.stdinSeen()
		return eof
	}, 5*time.Second, 10*time.Millisecond)
	fake.exit(0)

	require.NoError(t, cmd.Wait())

	seen, eof := fake.stdinSeen()
	assert.Equal(t, "one\ntwo\n", seen)
	assert.True(t, eof)
}

// TestCmdStdinChunked pins that a reader larger than one chunk still arrives
// whole.
func TestCmdStdinChunked(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	want := strings.Repeat("x", stdinChunkSize*2+7)

	cmd := target.Command(t.Context(), "cat")
	cmd.Stdin = strings.NewReader(want)
	cmd.Stdout = io.Discard

	require.NoError(t, cmd.Start())
	require.Eventually(t, func() bool {
		_, eof := fake.stdinSeen()
		return eof
	}, 5*time.Second, 10*time.Millisecond)
	fake.exit(0)
	require.NoError(t, cmd.Wait())

	seen, _ := fake.stdinSeen()
	assert.Equal(t, want, seen)
}

// TestCmdExitStatus pins that a command that exits non-zero is reported as
// such, with the status it exited with and everything it wrote.
func TestCmdExitStatus(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	fake.write("", "no such file\n")
	fake.exit(2)

	var stdout, stderr bytes.Buffer
	cmd := target.Command(t.Context(), "ls", "/nope")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	var exit *ExitError
	err := cmd.Run()
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 2, exit.Code)
	assert.Equal(t, 2, exit.ExitCode())
	assert.Equal(t, "cmd-1", exit.UUID)
	assert.Equal(t, 2, cmd.ExitCode)
	assert.Equal(t, "no such file\n", stderr.String())
}

// TestCmdExitStatusZero pins that a command that exits zero is no error, and
// that the status is readable all the same.
func TestCmdExitStatusZero(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)
	fake.exit(0)

	cmd := target.Command(t.Context(), "true")
	cmd.Stdout = io.Discard

	require.NoError(t, cmd.Run())
	assert.Equal(t, 0, cmd.ExitCode)
}

// TestCmdCancelWithinWaitDelay pins that a command that dies of the interrupt
// within the delay is reported as having ended, with its output and the
// status it died with rather than the cancellation.
func TestCmdCancelWithinWaitDelay(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	ctx, cancel := context.WithCancel(t.Context())
	var stdout bytes.Buffer
	cmd := target.Command(ctx, "sleep", "300")
	cmd.Stdout = &stdout
	cmd.WaitDelay = 10 * time.Second
	cmd.Cancel = func() error {
		// A command that takes the interrupt: it writes a last line and dies
		// of it, as one killed by SIGINT does.
		fake.write("interrupted\n", "")
		fake.exit(130)
		return nil
	}

	require.NoError(t, cmd.Start())
	cancel()

	var exit *ExitError
	require.ErrorAs(t, cmd.Wait(), &exit)
	assert.Equal(t, 130, exit.Code)
	assert.Equal(t, "interrupted\n", stdout.String())
}

// TestCmdCancelWaitDelayExpires pins that a command that ignores the interrupt
// is given up on once the delay is out, and is still addressable by UUID.
func TestCmdCancelWaitDelayExpires(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	ctx, cancel := context.WithCancel(t.Context())
	cmd := target.Command(ctx, "sleep", "300")
	cmd.Stdout = io.Discard
	cmd.WaitDelay = 50 * time.Millisecond

	require.NoError(t, cmd.Start())
	cancel()

	err := cmd.Wait()
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "cmd-1")
	assert.Equal(t, []int{int(syscall.SIGINT)}, fake.sentSignals())
}

// TestCmdCancelError pins that an error from Cancel ends the wait with it.
func TestCmdCancelError(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	ctx, cancel := context.WithCancel(t.Context())
	cmd := target.Command(ctx, "sleep", "300")
	cmd.Stdout = io.Discard
	cmd.WaitDelay = WaitForever

	sentinel := errors.New("could not interrupt")
	cmd.Cancel = func() error { return sentinel }

	require.NoError(t, cmd.Start())
	cancel()

	assert.ErrorIs(t, cmd.Wait(), sentinel)
}

// TestCmdOutputStreamedWhileRunning pins that output is delivered as the
// command produces it, not only once it has ended.
func TestCmdOutputStreamedWhileRunning(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)

	seen := make(chan struct{})
	cmd := target.Command(t.Context(), "sh", "-c", "...")
	cmd.Stdout = writerFunc(func(p []byte) (int, error) {
		if strings.Contains(string(p), "early") {
			select {
			case <-seen:
			default:
				close(seen)
			}
		}
		return len(p), nil
	})

	require.NoError(t, cmd.Start())
	fake.write("early\n", "")

	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("output was not delivered while the command was still running")
	}

	fake.exit(0)
	require.NoError(t, cmd.Wait())
}

// TestCmdMisuse pins what a Cmd refuses: no command, a second Start, a Wait
// without a Start, and a second Wait.
func TestCmdMisuse(t *testing.T) {
	fake := newFakePlugin()
	target := newTarget(t, fake)
	fake.exit(0)

	t.Run("no-command", func(t *testing.T) {
		cmd := target.CommandLine(t.Context(), nil)
		require.Error(t, cmd.Err)
		assert.ErrorContains(t, cmd.Run(), "no command given")
	})

	t.Run("wait-without-start", func(t *testing.T) {
		cmd := target.Command(t.Context(), "true")
		assert.ErrorContains(t, cmd.Wait(), "not started")
	})

	t.Run("double-start", func(t *testing.T) {
		cmd := target.Command(t.Context(), "true")
		cmd.Stdout = io.Discard
		require.NoError(t, cmd.Start())
		require.ErrorContains(t, cmd.Start(), "already started")
		require.NoError(t, cmd.Wait())
	})

	t.Run("double-wait", func(t *testing.T) {
		cmd := target.Command(t.Context(), "true")
		cmd.Stdout = io.Discard
		require.NoError(t, cmd.Run())
		assert.ErrorContains(t, cmd.Wait(), "already called")
	})
}

// TestCmdStartFailure pins that a plugin that does not report a UUID is a
// failure to start, named as such.
func TestCmdStartFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":{"uuid":""}}`)
	}))
	t.Cleanup(srv.Close)

	target := Target{
		Client:   plugin.NewClient(),
		Instance: platform.Instance{Uuid: "inst-1"},
		Plugin:   "sandbox",
		Opts: []plugin.Option{
			plugin.WithEndpoint(srv.URL),
			plugin.WithPluginName("sandbox"),
			plugin.WithHTTPClient(srv.Client()),
		},
	}

	err := target.Command(t.Context(), "true").Run()
	require.ErrorContains(t, err, "did not report a command UUID")
	assert.ErrorContains(t, err, `"sandbox"`)
}

// TestQuote pins that every argument reaches the command as one word, with
// nothing in it split, expanded or run.
func TestQuote(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"single", []string{"ls"}, `'ls'`},
		{"multiple", []string{"echo", "hello"}, `'echo' 'hello'`},
		{"spaces", []string{"echo", "two words"}, `'echo' 'two words'`},
		{"glob", []string{"echo", "*"}, `'echo' '*'`},
		{"dollar", []string{"echo", "$HOME"}, `'echo' '$HOME'`},
		{"backtick", []string{"echo", "`id`"}, "'echo' '`id`'"},
		{"semicolon", []string{"echo", "; rm -rf /"}, `'echo' '; rm -rf /'`},
		{"single-quote", []string{"echo", "it's"}, `'echo' 'it'\''s'`},
		{"quote-and-dollar", []string{"echo", "it's $HOME"}, `'echo' 'it'\''s $HOME'`},
		{"backslash", []string{"echo", `back\slash`}, `'echo' 'back\slash'`},
		{"empty", []string{"echo", ""}, `'echo' ''`},
		{"newline", []string{"printf", "a\nb"}, "'printf' 'a\nb'"},
		{"tab", []string{"printf", "a\tb"}, "'printf' 'a\tb'"},
		{"control", []string{"printf", "x\x01y"}, "'printf' 'x\x01y'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Quote(tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "$'")
		})
	}

	t.Run("nul-byte", func(t *testing.T) {
		_, err := Quote([]string{"echo", "a\x00b"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "NUL byte")
	})
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
