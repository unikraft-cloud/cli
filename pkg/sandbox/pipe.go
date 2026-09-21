// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"context"
	"errors"
	"io"
	"syscall"

	"unikraft.com/x/log"
)

// deadPipe is one of a command's output streams, kept usable after whoever was
// reading it has gone: a `| head` that has read enough, or a shell pipeline
// whose right-hand side ended first.
type deadPipe struct {
	out    io.Writer
	closed func()
	dead   bool
}

func (w *deadPipe) Write(p []byte) (int, error) {
	if w.dead {
		return len(p), nil
	}
	n, err := w.out.Write(p)
	switch {
	case err == nil:
		return n, nil
	case errors.Is(err, syscall.EPIPE), errors.Is(err, io.ErrClosedPipe):
		// The reader has gone: the command is told, and from here its output
		// goes nowhere.
		w.dead = true
		w.closed()
		return len(p), nil
	default:
		// A writer that failed is not a reader that left: a full disk behind
		// "> file" is the command's failure to report.
		return n, err
	}
}

// outputTo wraps one of the command's output streams against its reader going
// away
func (c *Cmd) outputTo(w io.Writer) io.Writer {
	if w == nil {
		return nil
	}
	return &deadPipe{out: w, closed: c.pipeClosed}
}

// pipeClosed tells the instance that nobody is reading the command's output any
// more
func (c *Cmd) pipeClosed() {
	c.pipeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), signalTimeout)
		defer cancel()

		if err := c.Signal(ctx, syscall.SIGPIPE); err != nil {
			log.G(c.ctx).Debug().Err(err).Str("cmd", c.UUID).Msg("could not report the closed pipe")
		}
	})
}
