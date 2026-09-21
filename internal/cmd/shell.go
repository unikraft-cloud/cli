// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MakeNowJust/heredoc"

	"unikraft.com/x/kingkong"
	"unikraft.com/x/shell"
	xsignal "unikraft.com/x/signal"
	xstdio "unikraft.com/x/stdio"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/pkg/sandbox"
)

const shellBanner = "⚠︎ this shell is experimental"

type ShellSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to open a shell on."`

	Plugin  string   `name:"plugin" default:"${sandbox_plugin}" help:"Name of the sandbox plugin to use." placeholder:"name"`
	Dir     string   `name:"dir" short:"w" default:"/" help:"Directory to start the shell in." placeholder:"dir"`
	Env     []string `name:"env" short:"e" sep:"none" help:"Environment variable." placeholder:"<key>=<value>" example:"DEBUG=true"`
	Command string   `name:"command" short:"c" help:"Run a single command line and exit." placeholder:"line"`

	InterruptGrace time.Duration `name:"interrupt-grace" default:"10s" placeholder:"duration" help:"How long an interrupted command is given to report what it died of before the prompt comes back."`
	ReapTimeout    time.Duration `name:"reap-timeout" default:"30s" placeholder:"duration" help:"How long a command the shell stopped waiting for is given to finish in the background."`
}

func (ShellSandboxInstanceCmd) Help() string {
	return heredoc.Docf(`
		The shell runs locally and the commands you type run on the instance.
		Session state — the working directory, variables, functions, %[1]s$?%[1]s — is kept
		here, while paths resolve against the instance, so %[1]scd%[1]s, %[1]s*.log%[1]s and %[1]s> file%[1]s
		all mean what you would expect.

		The instance offers no terminal and no job control, so programs that need
		one — %[1]svim%[1]s, %[1]stop%[1]s, %[1]sless%[1]s — and %[1]sctrl-z%[1]s, %[1]sbg%[1]s or %[1]sfg%[1]s will not work there yet.
	`, "`")
}

func (ShellSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Open an interactive shell on a sandbox instance",
			Commands: []string{
				"unikraft instance shell my-instance",
			},
		},
		{
			Description: "Start the shell in a specific working directory",
			Commands: []string{
				"unikraft instance shell my-instance --dir /var/lib/app",
			},
		},
		{
			Description: "Run a single command line and exit",
			Commands: []string{
				`unikraft instance shell my-instance -c 'cd /var/log && ls *.log'`,
			},
		},
	}
}

func (c *ShellSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition, signals *xsignal.Signals) error {
	env, err := parseEnv(c.Env)
	if err != nil {
		return err
	}

	target, err := resolveSandboxTarget(ctx, partition, c.Target, c.Plugin)
	if err != nil {
		return err
	}

	code, err := shell.Run(ctx, shell.Config{
		Instance:       c.Target,
		Dir:            c.Dir,
		Env:            env,
		Command:        c.Command,
		Transport:      sandbox.Transport{Target: target, InterruptGrace: c.InterruptGrace, ReapTimeout: c.ReapTimeout}.Shell(),
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

type ExitStatus int

func (c ExitStatus) Error() string {
	return fmt.Sprintf("exited with status %d", int(c))
}

func (c ExitStatus) ExitCode() int { return int(c) }
