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

	"unikraft.com/cloud/sdk/plugins/sandbox"

	"unikraft.com/x/kingkong"
	"unikraft.com/x/shell"
	"unikraft.com/x/shell/builtins"
	xsignal "unikraft.com/x/signal"
	xstdio "unikraft.com/x/stdio"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
	xkong "unikraft.com/cli/internal/x/kong"
	"unikraft.com/cli/pkg/shellbuiltins"
)

const shellBanner = "⚠︎ this shell is experimental"

type ShellSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to open a shell on."`

	SandboxPluginOpts
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

		A line starting with %[1]s:%[1]s is a builtin — %[1]s:get%[1]s, %[1]s:restart%[1]s, %[1]s:mount%[1]s and the
		like; %[1]s:help%[1]s lists them.

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

// ShellInstance runs the shell's builtins with the CLI's own commands.
type ShellInstance struct {
	Key       string
	Partition *resource.Partition
}

func (i ShellInstance) Get(ctx context.Context, stdio xstdio.Stdio, format builtins.Format) error {
	opts, err := formatOpts(format)
	if err != nil {
		return err
	}
	return (&cmd.ResourceGetCmd[Instance]{Targets: []string{i.Key}, FormatOpts: opts}).Run(ctx, cliStdio(stdio), i.Partition)
}

func (i ShellInstance) Volumes(ctx context.Context, stdio xstdio.Stdio, format builtins.Format) error {
	opts, err := formatOpts(format)
	if err != nil {
		return err
	}
	return (&cmd.ResourceListCmd[Volume]{FormatOpts: opts}).Run(ctx, cliStdio(stdio), i.Partition)
}

func (i ShellInstance) Edit(ctx context.Context, stdio xstdio.Stdio, set map[string]string) error {
	return (&cmd.ResourceEditCmd[Instance]{Target: i.Key, Set: []map[string]string{set}}).Run(ctx, cliStdio(stdio), i.Partition)
}

func (i ShellInstance) Attach(ctx context.Context, stdio xstdio.Stdio, volume, at string, readonly bool) error {
	return (&VolumeAttachCmd{Volume: volume, To: i.Key, At: at, Readonly: readonly}).Run(ctx, cliStdio(stdio), i.Partition)
}

func (i ShellInstance) Detach(ctx context.Context, stdio xstdio.Stdio, volume string) error {
	return (&VolumeDetachCmd{Volume: volume, From: i.Key}).Run(ctx, cliStdio(stdio), i.Partition)
}

func (i ShellInstance) Start(ctx context.Context, stdio xstdio.Stdio) error {
	return (&InstancesStartCmd{Targets: []string{i.Key}}).Run(ctx, cliStdio(stdio))
}

func (i ShellInstance) Stop(ctx context.Context, stdio xstdio.Stdio, opts shellbuiltins.StopOpts) error {
	return (&InstancesStopCmd{Targets: []string{i.Key}, StopOpts: stopOpts(opts)}).Run(ctx, cliStdio(stdio))
}

func (i ShellInstance) Restart(ctx context.Context, stdio xstdio.Stdio, opts shellbuiltins.StopOpts) error {
	return (&InstancesRestartCmd{Targets: []string{i.Key}, StopOpts: stopOpts(opts)}).Run(ctx, cliStdio(stdio))
}

func (i ShellInstance) Suspend(ctx context.Context, stdio xstdio.Stdio, drainTimeout types.DurationMS) error {
	return (&InstancesSuspendCmd{Targets: []string{i.Key}, DrainTimeout: drainTimeout}).Run(ctx, cliStdio(stdio))
}

func cliStdio(s xstdio.Stdio) config.Stdio {
	return config.Stdio{Stdin: s.Stdin, Stdout: s.Stdout, Stderr: s.Stderr}
}

func formatOpts(f builtins.Format) (cmd.FormatOpts, error) {
	printer, err := cmd.ParsePrinter(f.Output)
	if err != nil {
		return cmd.FormatOpts{}, err
	}
	return cmd.FormatOpts{Field: xkong.GreedyStrings(f.Field), Output: printer}, nil
}

func stopOpts(o shellbuiltins.StopOpts) StopOpts {
	return StopOpts{Force: o.Force, DrainTimeout: o.DrainTimeout}
}

type ExitStatus int

func (c ExitStatus) Error() string {
	return fmt.Sprintf("exited with status %d", int(c))
}

func (c ExitStatus) ExitCode() int { return int(c) }
