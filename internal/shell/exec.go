// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"context"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
)

const (
	// BuiltinSigil opens a line the shell answers through [Builtins].
	BuiltinSigil = ":"

	statusNotFound = 127

	// StatusInterrupted is what a command killed by a signal reports.
	StatusInterrupted = 130

	maxSignal = 64
)

var sessionBuiltinNames = []string{"history"}

func (s *session) route(_ interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(ctx context.Context, args []string) error {
		if len(args[0]) > len(BuiltinSigil) && strings.HasPrefix(args[0], BuiltinSigil) {
			return s.runBuiltin(ctx, args)
		}
		return s.runRemote(ctx, args)
	}
}

func (s *session) runRemote(ctx context.Context, args []string) error {
	hc := interp.HandlerCtx(ctx)

	ctx, done := context.WithCancel(ctx)
	defer done()

	streams := streamsOf(hc)
	streams.In = s.commandStdin(ctx, streams.In)

	code, err := s.cfg.Transport.Exec(ctx, streams, hc.Dir, exported(hc.Env), args)
	if err != nil {
		if ctx.Err() != nil {
			return interp.ExitStatus(StatusInterrupted)
		}
		fmt.Fprintln(hc.Stderr, errorStyle.Render(err.Error()))
		return interp.ExitStatus(1)
	}
	return exitStatus(code)
}

func (s *session) runBuiltin(ctx context.Context, args []string) error {
	hc := interp.HandlerCtx(ctx)
	streams := streamsOf(hc)

	args = append([]string{strings.TrimPrefix(args[0], BuiltinSigil)}, args[1:]...)
	if slices.Contains(sessionBuiltinNames, args[0]) {
		s.runSessionBuiltin(streams, args)
		return nil
	}

	if s.cfg.Builtins == nil {
		fmt.Fprintln(hc.Stderr, errorStyle.Render("unknown builtin: "+args[0]))
		return interp.ExitStatus(statusNotFound)
	}

	code, err := s.cfg.Builtins.Run(ctx, streams, args)
	if err != nil {
		fmt.Fprintln(hc.Stderr, errorStyle.Render(err.Error()))
		if code == 0 {
			code = 1
		}
	}
	if args[0] == "help" && err == nil {
		s.printSessionBuiltins(streams.Out)
	}
	if err == nil && s.builtinRestarts(args) {
		s.restarted.Store(true)
		if !s.settle(ctx) {
			return interp.ExitStatus(StatusInterrupted)
		}
	}
	return exitStatus(code)
}

func (s *session) builtinRestarts(args []string) bool {
	lifecycle, ok := s.cfg.Builtins.(Restarts)
	return ok && lifecycle.Restarts(args)
}

func (s *session) runSessionBuiltin(streams Streams, args []string) {
	switch args[0] {
	case "history":
		if s.editor == nil {
			return
		}
		for i, line := range s.editor.history.recalled() {
			fmt.Fprintf(streams.Out, "%5d  %s\n", i+1, line)
		}
	}
}

func (s *session) printSessionBuiltins(out io.Writer) {
	fmt.Fprintf(out, "  %-34s %s\n", BuiltinSigil+"history", "List what this session has run.")
	fmt.Fprintln(out, "\nEverything else runs on the instance.")
}

func resolve(dir, p string) string {
	if !path.IsAbs(p) {
		p = path.Join(dir, p)
	}
	return path.Clean(p)
}

func (s *session) commandStdin(ctx context.Context, in io.Reader) io.Reader {
	switch {
	case in != nil:
		return in
	case s.tty != nil:
		return terminalStdin(ctx, s.tty)
	default:
		return nil
	}
}

func streamsOf(hc interp.HandlerContext) Streams {
	return Streams{In: hc.Stdin, Out: hc.Stdout, Err: hc.Stderr}
}

func exported(env expand.Environ) map[string]string {
	vars := map[string]string{}
	env.Each(func(name string, vr expand.Variable) bool {
		if vr.Exported && vr.IsSet() {
			vars[name] = vr.String()
		}
		return true
	})
	return vars
}

func exitStatus(code int) error {
	switch {
	case code == 0:
		return nil
	case code < 0 && code >= -maxSignal:
		return interp.ExitStatus(128 - code)
	case code < 0 || code > 255:
		return interp.ExitStatus(1)
	default:
		return interp.ExitStatus(code)
	}
}
