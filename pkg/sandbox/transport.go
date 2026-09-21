// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"unikraft.com/x/log"
	"unikraft.com/x/shell"
)

const (
	// DefaultInterruptGrace is how long an interrupted command gets to
	// report its end before it is killed.
	DefaultInterruptGrace = 10 * time.Second

	// DefaultReapTimeout is how long a command nobody waits for is given
	// to die.
	DefaultReapTimeout = 30 * time.Second
)

// Transport runs the shell's commands on the instance through the plugin.
// A command's exit is a status to report, not an error.
type Transport struct {
	Target Target

	// InterruptGrace and ReapTimeout bound how long the shell waits on a
	// command it has given up on. Zero is the default, not forever.
	InterruptGrace time.Duration
	ReapTimeout    time.Duration
}

// Shell answers the shell's file and environment questions through sh on
// the instance, given Exec to run each command.
func (t Transport) Shell() shell.ExecTransport { return t.Exec }

// Exec runs one command and reports its status, not an error, when it ran.
func (t Transport) Exec(ctx context.Context, sc shell.Command) (int, error) {
	detach := shell.IsDetached(ctx)

	cmd := t.Target.CommandArgs(ctx, sc.Args)
	cmd.Dir = sc.Dir
	cmd.Env = sc.Env
	cmd.Stdin = sc.Streams.Stdin
	cmd.Stdout = sc.Streams.Stdout
	cmd.Stderr = sc.Streams.Stderr

	cmd.WaitDelay = cmp.Or(t.InterruptGrace, DefaultInterruptGrace)
	if detach {
		cmd.WaitDelay = cmp.Or(t.ReapTimeout, DefaultReapTimeout)
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
		go t.reap(context.WithoutCancel(ctx), cmd, done)
		return 0, ctx.Err()

	case err := <-done:
		code, err := shell.ExitStatus(err)
		switch {
		case err == nil:
			return code, nil
		case ctx.Err() == nil:
			return 0, err
		default:
			fmt.Fprintln(sc.Streams.Stderr, err)
			return shell.StatusInterrupted, nil
		}
	}
}

// reap waits out a command the shell no longer waits for, logs a bad end and
// drops the instance's record of it.
func (t Transport) reap(ctx context.Context, cmd *Cmd, done <-chan error) {
	if err := <-done; err != nil {
		log.G(ctx).Debug().Err(err).Str("cmd", cmd.UUID).Msg("the interrupted command did not finish")
	}
	cmd.Forget(ctx)
}
