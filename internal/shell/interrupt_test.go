// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"context"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

const blockingCommand = "block"

// haltingTransport stands in for a command that only stops when it is told to,
// and reports the status the instance would: a signal as its negation. It
// records whether each command arrived detached.
type haltingTransport struct {
	started  chan struct{}
	starting sync.Once

	mu       sync.Mutex
	ran      []string
	detached []bool
}

func newHaltingTransport() *haltingTransport {
	return &haltingTransport{started: make(chan struct{})}
}

func (t *haltingTransport) Exec(ctx context.Context, _ Streams, _ string, _ map[string]string, args []string) (int, error) {
	t.mu.Lock()
	t.ran = append(t.ran, strings.Join(args, " "))
	t.detached = append(t.detached, IsDetached(ctx))
	t.mu.Unlock()

	if args[0] != blockingCommand {
		return 0, nil
	}
	t.starting.Do(func() { close(t.started) })

	select {
	case <-ctx.Done():
		return -int(syscall.SIGINT), nil
	case <-time.After(10 * time.Second):
		return 0, nil
	}
}

func (t *haltingTransport) commands() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.ran...)
}

func (t *haltingTransport) lastDetached() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.detached) > 0 && t.detached[len(t.detached)-1]
}

func newHaltingSession(t *testing.T, transport Transport) (*session, *captured) {
	t.Helper()

	out := &captured{}
	s := &session{
		cfg:     Config{Transport: transport},
		console: console{Out: out, Err: out},
	}

	runner, err := interp.New(
		interp.StdIO(nil, out, out),
		interp.ExecHandlers(s.route),
	)
	require.NoError(t, err)

	runner.Dir = "/"
	runner.Reset()
	s.runner = runner
	return s, out
}

func parseShell(t *testing.T, src string) []*syntax.Stmt {
	t.Helper()
	prog, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	require.NoError(t, err)
	return prog.Stmts
}

func interruptOnce(t *haltingTransport, sigint chan os.Signal) {
	go func() {
		<-t.started
		sigint <- syscall.SIGINT
	}()
}

func runShellLine(t *testing.T, s *session, sigint chan os.Signal, src string) bool {
	t.Helper()

	for _, stmt := range parseShell(t, src) {
		interrupted, err := s.runStmt(t.Context(), sigint, stmt)
		require.NoError(t, err)
		if interrupted {
			return true
		}
	}
	return false
}

func status(t *testing.T, s *session, out *captured) string {
	t.Helper()

	before := len(out.String())
	require.NoError(t, dropExitStatus(s.runner.Run(t.Context(), &syntax.File{
		Stmts: parseShell(t, `printf 'status=%s' "$?"`),
	})))
	reported, found := strings.CutPrefix(out.String()[before:], "status=")
	require.True(t, found, "printf did not report the status")
	return reported
}

func TestInterruptStopsTheCommand(t *testing.T) {
	transport := newHaltingTransport()
	s, out := newHaltingSession(t, transport)

	sigint := make(chan os.Signal, 4)
	interruptOnce(transport, sigint)

	assert.True(t, runShellLine(t, s, sigint, blockingCommand))
	assert.Equal(t, "130", status(t, s, out))
}

func TestInterruptKeepsTheSession(t *testing.T) {
	for _, tt := range []struct {
		name string
		line string
	}{
		{"while-loop", "while true; do " + blockingCommand + "; done"},
		{"block", "{ " + blockingCommand + "; echo unreachable; }"},
		{"function-body", "f() { " + blockingCommand + "; echo unreachable; }; f"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			transport := newHaltingTransport()
			s, out := newHaltingSession(t, transport)

			sigint := make(chan os.Signal, 4)
			interruptOnce(transport, sigint)

			assert.True(t, runShellLine(t, s, sigint, tt.line))
			assert.False(t, s.runner.Exited(), "the shell must survive its own Ctrl-C")
			assert.Equal(t, "130", status(t, s, out))
			assert.NotContains(t, out.String(), "unreachable")
		})
	}
}

func TestInterruptAbandonsTheLine(t *testing.T) {
	transport := newHaltingTransport()
	s, _ := newHaltingSession(t, transport)

	sigint := make(chan os.Signal, 4)
	interruptOnce(transport, sigint)

	assert.True(t, runShellLine(t, s, sigint, blockingCommand+"; second"))
	assert.Equal(t, []string{blockingCommand}, transport.commands())
}

func TestRepeatedInterruptsStillStopTheCommand(t *testing.T) {
	transport := newHaltingTransport()
	s, out := newHaltingSession(t, transport)

	sigint := make(chan os.Signal, 4)
	go func() {
		<-transport.started
		for range 3 {
			sigint <- syscall.SIGINT
		}
	}()

	assert.True(t, runShellLine(t, s, sigint, blockingCommand))
	assert.Equal(t, "130", status(t, s, out))
}
