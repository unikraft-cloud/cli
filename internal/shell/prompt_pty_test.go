// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperEnv puts the test binary into a mode where it is the shell itself,
// reading the terminal it was given. The prompt reads the process' terminal,
// so driving it means driving another process.
const helperEnv = "SHELL_PROMPT_HELPER"

func TestMain(m *testing.M) {
	if root := os.Getenv(helperEnv); root != "" {
		os.Exit(runHelperSession(root))
	}
	os.Exit(m.Run())
}

func runHelperSession(root string) int {
	err := Run(context.Background(), Config{
		Instance:  "fake",
		Dir:       root,
		Transport: localTransport{},
		Builtins:  namedBuiltins{"start"},
	}, Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// terminal is the test's end of a session running under a pty.
type terminal struct {
	t    *testing.T
	ptmx *os.File
	cmd  *exec.Cmd

	mu  sync.Mutex
	buf strings.Builder
}

func newTerminal(t *testing.T, root string) *terminal {
	t.Helper()

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"="+root)

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err)
	require.NoError(t, pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 120}))

	term := &terminal{t: t, ptmx: ptmx, cmd: cmd}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				term.mu.Lock()
				term.buf.Write(buf[:n])
				term.mu.Unlock()
				term.answer(string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	t.Cleanup(func() {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	term.await("$ ")
	return term
}

// answer replies to what a terminal is asked. Styling asks for the background
// colour on startup, and the prompt asks where the cursor is before it draws;
// both block until they have an answer, so nothing typed reaches the prompt
// until they do.
func (term *terminal) answer(out string) {
	if strings.Contains(out, "\x1b]11;?") {
		_, _ = term.ptmx.WriteString("\x1b]11;rgb:0000/0000/0000\a")
	}
	if strings.Contains(out, "\x1b[c") {
		_, _ = term.ptmx.WriteString("\x1b[?1;2c")
	}
	if strings.Contains(out, "\x1b[6n") {
		_, _ = term.ptmx.WriteString("\x1b[1;1R")
	}
}

func (term *terminal) seen() string {
	term.mu.Lock()
	defer term.mu.Unlock()
	return ansi.Strip(term.buf.String())
}

// forget drops what has been seen so far, so that what comes next can be
// asserted on its own.
func (term *terminal) forget() {
	term.mu.Lock()
	defer term.mu.Unlock()
	term.buf.Reset()
}

func (term *terminal) send(keys string) {
	term.t.Helper()
	_, err := term.ptmx.WriteString(keys)
	require.NoError(term.t, err)
}

func (term *terminal) await(want string) {
	term.t.Helper()

	seen := assert.Eventually(term.t, func() bool {
		return strings.Contains(term.seen(), want)
	}, 10*time.Second, 20*time.Millisecond)

	if !seen {
		term.t.Errorf("never saw %q in:\n%s", want, term.seen())
	}
}

// wait lets the session finish, so that what it printed last is in hand.
func (term *terminal) wait() string {
	term.t.Helper()

	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, term.ptmx); close(done) }()
	_ = term.cmd.Wait()
	<-done

	return term.seen()
}

func TestPrompt(t *testing.T) {
	root := newFixture(t)

	t.Run("says-what-it-cannot-do", func(t *testing.T) {
		term := newTerminal(t, root)

		term.await("experimental")
		term.await("no job control")
	})

	t.Run("runs-a-line-on-the-instance", func(t *testing.T) {
		term := newTerminal(t, root)

		term.send("echo hello\r")
		term.await("hello")
	})

	t.Run("keeps-the-working-directory", func(t *testing.T) {
		term := newTerminal(t, root)

		term.send("cd var/log\r")
		term.await("/var/log$ ")
		term.send("pwd\r")
		term.await(root + "/var/log")
	})

	t.Run("recalls-the-previous-line", func(t *testing.T) {
		term := newTerminal(t, root)

		term.send("echo alpha\r")
		term.await("alpha")

		term.forget()
		term.send("\x1b[A") // up
		term.await("echo alpha")
		term.send("\r")
		term.await("alpha")
	})

	t.Run("searches-what-it-has-run", func(t *testing.T) {
		term := newTerminal(t, root)

		term.send("echo needle\r")
		term.await("needle")
		term.send("\x12needle") // ctrl-r
		term.await("echo needle")
		term.send("\r")
		term.await("needle")
	})

	t.Run("completes-a-path-on-the-instance", func(t *testing.T) {
		term := newTerminal(t, root)

		term.send("cat hostna\t")
		term.await("hostname")
		term.send("\r")
		term.await("fakebox")
	})

	t.Run("keeps-reading-an-incomplete-line", func(t *testing.T) {
		term := newTerminal(t, root)

		term.send("for i in 1 2 3; do\r")
		term.await("for i in 1 2 3; do")
		term.forget()

		term.send("printf %s $i\r")
		term.send("done\r")
		term.await("123")
	})

	t.Run("interrupts-the-command-and-keeps-the-session", func(t *testing.T) {
		term := newTerminal(t, root)

		term.send("sleep 30\r")
		time.Sleep(300 * time.Millisecond)
		term.send("\x03") // ctrl-c
		term.await("^C")

		term.send("echo after\r")
		term.await("after")
	})

	t.Run("leaves-on-ctrl-d", func(t *testing.T) {
		term := newTerminal(t, root)

		term.send("echo bye\r")
		term.await("bye")
		term.send("\x04") // ctrl-d

		term.wait()
		assert.Zero(t, term.cmd.ProcessState.ExitCode(), "a clean exit")
	})
}
