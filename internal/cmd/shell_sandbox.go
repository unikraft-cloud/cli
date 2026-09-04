// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"syscall"
	"time"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/sandbox"
	"unikraft.com/cli/internal/shell"
)

const (
	// sandboxReapTimeout is how long a probe nobody waits for is given to die.
	sandboxReapTimeout = 30 * time.Second

	sandboxSignalTimeout = 5 * time.Second

	// sandboxInterruptGrace is how long an interrupted command is given to report what it died of.
	sandboxInterruptGrace = 10 * time.Second
)

// sandboxTransport is the instance as the shell reaches it: a status to report rather than an error.
type sandboxTransport struct {
	target sandbox.Target
}

func (t sandboxTransport) Exec(ctx context.Context, streams shell.Streams, dir string, env map[string]string, args []string) (int, error) {
	detach := shell.IsDetached(ctx)

	cmd := t.target.CommandLine(ctx, args)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = streams.In
	var told sync.Once
	closedPipe := func() {
		told.Do(func() {
			signalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sandboxSignalTimeout)
			defer cancel()

			if err := cmd.Signal(signalCtx, syscall.SIGPIPE); err != nil {
				log.G(ctx).Debug().Err(err).Str("cmd", cmd.UUID).Msg("could not report the closed pipe")
			}
		})
	}

	cmd.Stdout = &lastingWriter{out: streams.Out, closed: closedPipe}
	cmd.Stderr = &lastingWriter{out: streams.Err, closed: closedPipe}

	cmd.WaitDelay = sandboxInterruptGrace
	if detach {
		cmd.WaitDelay = sandboxReapTimeout
	}

	if err := cmd.Start(); err != nil {
		return 0, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var detached <-chan struct{}
	if detach {
		detached = ctx.Done()
	}

	select {
	case <-detached:
		go t.reapCommand(context.WithoutCancel(ctx), cmd, done)
		return 0, ctx.Err()

	case err := <-done:
		var exit *sandbox.ExitError
		switch {
		case err == nil:
			return 0, nil
		case errors.As(err, &exit):
			return exit.Code, nil
		case ctx.Err() == nil:
			return 0, err
		default:
			fmt.Fprintln(streams.Err, err)
			return shell.StatusInterrupted, nil
		}
	}
}

// reapCommand finishes with a command the shell has stopped waiting for.
func (t sandboxTransport) reapCommand(ctx context.Context, cmd *sandbox.Cmd, done <-chan error) {
	if err := <-done; err != nil {
		log.G(ctx).Debug().Err(err).Str("cmd", cmd.UUID).Msg("the interrupted command did not finish")
		return
	}
	cmd.Forget(ctx)
}

// lastingWriter keeps taking bytes after the writer it wraps stops, telling the instance the once.
type lastingWriter struct {
	out    io.Writer
	closed func()
	dead   bool
}

func (w *lastingWriter) Write(p []byte) (int, error) {
	if w.dead || w.out == nil {
		return len(p), nil
	}
	if _, err := w.out.Write(p); err != nil {
		w.dead = true
		w.closed()
	}
	return len(p), nil
}
