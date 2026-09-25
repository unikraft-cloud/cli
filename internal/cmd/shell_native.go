// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !windows

package cmd

import (
	"context"
	"errors"

	"unikraft.com/cloud/sdk/plugins/sandbox"

	"unikraft.com/x/shell"
	xsignal "unikraft.com/x/signal"
	xstdio "unikraft.com/x/stdio"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/pkg/shellbuiltins"
)

const shellBanner = "⚠︎ this shell is experimental"

func (c *ShellSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition, signals *xsignal.Signals) error {
	env, err := parseEnv(c.Env)
	if err != nil {
		return err
	}

	target, err := resolveSandboxTarget(ctx, stdio, partition, c.Target, c.SandboxPluginOpts)
	if err != nil {
		return err
	}
	builtins, err := shellbuiltins.New(ShellInstance{Key: c.Target, Partition: partition}, target)
	if err != nil {
		return err
	}

	code, err := shell.Run(ctx, shell.Config{
		Instance: c.Target,
		Dir:      c.Dir,
		Env:      env,
		Command:  c.Command,
		Transport: sandbox.Transport{
			Target:   target,
			Timeouts: sandbox.Timeouts{InterruptGrace: c.InterruptGrace, Reap: c.ReapTimeout},
		}.Shell(),
		Builtins:       builtins,
		SuspendSignals: signals.Suspend,
		Banner:         shellBanner,
	}, xstdio.Stdio{Stdin: stdio.Stdin, Stdout: stdio.Stdout, Stderr: stdio.Stderr})
	switch {
	case err != nil && errors.Is(ctx.Err(), context.Canceled):
		// Ctrl-C ended the line, so the CLI ends the way a shell does: on the
		// status of the interrupt, rather than as a failure of its own.
		return ExitStatus(shell.StatusInterrupted)
	case err != nil || code == 0:
		return err
	}
	return ExitStatus(code)
}
