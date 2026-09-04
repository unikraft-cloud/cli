// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"unikraft.com/x/log"
	"unikraft.com/x/shell"

	"unikraft.com/cli/internal/sandbox"
)

// sandboxTimeouts is how long the shell waits on a command it has given up on.
type sandboxTimeouts struct {
	InterruptGrace time.Duration
	Reap           time.Duration
}

var defaultSandboxTimeouts = sandboxTimeouts{InterruptGrace: 10 * time.Second, Reap: 30 * time.Second}

// sandboxTransport is the instance as the shell reaches it: a status to report rather than an error.
type sandboxTransport struct {
	shell.ExecTransport
	target   sandbox.Target
	timeouts sandboxTimeouts
}

// newSandboxTransport reaches target; a zero timeout is the default one.
func newSandboxTransport(target sandbox.Target, timeouts sandboxTimeouts) *sandboxTransport {
	t := &sandboxTransport{target: target, timeouts: sandboxTimeouts{
		InterruptGrace: cmp.Or(timeouts.InterruptGrace, defaultSandboxTimeouts.InterruptGrace),
		Reap:           cmp.Or(timeouts.Reap, defaultSandboxTimeouts.Reap),
	}}
	// Bound to the pointer, so exec sees every field however late it is set.
	t.ExecTransport = t.exec
	return t
}

func (t *sandboxTransport) exec(ctx context.Context, command shell.Command) (int, error) {
	detach := shell.IsDetached(ctx)

	cmd := t.target.CommandArgs(ctx, command.Args)
	cmd.Dir = command.Dir
	cmd.Env = command.Env
	cmd.Stdin = command.Streams.Stdin
	cmd.Stdout = command.Streams.Stdout
	cmd.Stderr = command.Streams.Stderr

	cmd.WaitDelay = t.timeouts.InterruptGrace
	if detach {
		cmd.WaitDelay = t.timeouts.Reap
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
		code, err := shell.ExitStatus(err)
		switch {
		case err == nil:
			return code, nil
		case ctx.Err() == nil:
			return 0, err
		default:
			fmt.Fprintln(command.Streams.Stderr, err)
			return shell.StatusInterrupted, nil
		}
	}
}

// reapCommand finishes with a command the shell has stopped waiting for
func (t *sandboxTransport) reapCommand(ctx context.Context, cmd *sandbox.Cmd, done <-chan error) {
	if err := <-done; err != nil {
		log.G(ctx).Debug().Err(err).Str("cmd", cmd.UUID).Msg("the interrupted command did not finish")
	}
}
