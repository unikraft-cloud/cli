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
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/MakeNowJust/heredoc"
	"github.com/alecthomas/kong"

	"unikraft.com/x/colors"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/shell"
	"unikraft.com/x/shell/builtins"
	xsignal "unikraft.com/x/signal"
	xstdio "unikraft.com/x/stdio"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
)

const shellBanner = "⚠︎ this shell is experimental"

var restartHintStyle = lipgloss.NewStyle().Foreground(colors.Warning)

// restartingBuiltins take the instance down and up again, so the shell waits for it.
var restartingBuiltins = []string{"restart", "start"}

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

		A line starting with %[1]s:%[1]s is a builtin the CLI answers itself — %[1]s:get%[1]s, %[1]s:restart%[1]s,
		%[1]s:mount%[1]s and the like; %[1]s:help%[1]s lists them.

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
	answers, err := newShellBuiltins(shellBuiltins{key: c.Target, partition: partition})
	if err != nil {
		return err
	}

	code, err := shell.Run(ctx, shell.Config{
		Instance:       c.Target,
		Dir:            c.Dir,
		Env:            env,
		Command:        c.Command,
		Transport:      newSandboxTransport(target, sandboxTimeouts{InterruptGrace: c.InterruptGrace, Reap: c.ReapTimeout}),
		Builtins:       answers.Builtins(),
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

// shellBuiltins is what the builtins run against: the instance the shell was opened on.
type shellBuiltins struct {
	key       string
	partition *resource.Partition
}

// builtinList prints the grammar, for ":help" to report.
type builtinList func(io.Writer)

func newShellBuiltins(b shellBuiltins) (*builtins.Kong, error) {
	var answers *builtins.Kong

	answers, err := builtins.NewKong(builtins.KongConfig{
		Commands: func() any { return &shellBuiltinCmds{} },
		Options:  []kong.Option{kong.Description("Builtins run on this CLI rather than the instance.")},
		Bind: func(_ context.Context, streams xstdio.Stdio) []any {
			return []any{
				config.Stdio{Stdin: streams.Stdin, Stdout: streams.Stdout, Stderr: streams.Stderr},
				b,
				builtinList(answers.List),
			}
		},
		Restart: func(args []string) bool { return slices.Contains(restartingBuiltins, args[0]) },
	})
	return answers, err
}

type shellBuiltinCmds struct {
	Edit    shellEditBuiltin    `cmd:"" name:":edit" help:"Change this instance's settings."`
	Get     shellGetBuiltin     `cmd:"" name:":get" help:"Inspect this instance."`
	Help    shellHelpBuiltin    `cmd:"" name:":help" help:"List these builtins."`
	Mount   shellMountBuiltin   `cmd:"" name:":mount" help:"Attach a volume to this instance."`
	Restart shellRestartBuiltin `cmd:"" name:":restart" help:"Restart this instance."`
	Start   shellStartBuiltin   `cmd:"" name:":start" help:"Start this instance."`
	Stop    shellStopBuiltin    `cmd:"" name:":stop" help:"Stop this instance."`
	Suspend shellSuspendBuiltin `cmd:"" name:":suspend" help:"Suspend this instance."`
	Unmount shellUnmountBuiltin `cmd:"" name:":unmount" help:"Detach a volume from this instance."`
	Volumes shellVolumesBuiltin `cmd:"" name:":volumes" help:"List volumes."`
}

type shellGetBuiltin struct {
	cmd.FormatOpts
}

func (c shellGetBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&cmd.ResourceGetCmd[Instance]{
		Targets:    []string{b.key},
		FormatOpts: c.FormatOpts,
	}).Run(ctx, stdio, b.partition)
}

type shellVolumesBuiltin struct {
	cmd.FormatOpts
}

func (c shellVolumesBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&cmd.ResourceListCmd[Volume]{FormatOpts: c.FormatOpts}).Run(ctx, stdio, b.partition)
}

type shellEditBuiltin struct {
	Fields []string `arg:"" name:"field=value" help:"Fields to set on this instance."`
}

func (c shellEditBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	set := map[string]string{}
	for _, field := range c.Fields {
		name, value, ok := strings.Cut(field, "=")
		if !ok {
			return fmt.Errorf("%q is not <field>=<value>", field)
		}
		set[name] = value
	}

	if err := (&cmd.ResourceEditCmd[Instance]{
		Target: b.key,
		Set:    []map[string]string{set},
	}).Run(ctx, quiet(stdio), b.partition); err != nil {
		return err
	}

	fmt.Fprintln(stdio.Stderr, restartHint("the instance reads its settings at boot"))
	return nil
}

type shellMountBuiltin struct {
	Volume   string `arg:"" completion-predictor:"resource-key-volume" help:"Volume to attach."`
	At       string `arg:"" name:"path" help:"Absolute mount path inside the instance."`
	Readonly bool   `help:"Mount the volume as read-only."`
}

func (c shellMountBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	if err := (&VolumeAttachCmd{
		Volume:   c.Volume,
		To:       b.key,
		At:       c.At,
		Readonly: c.Readonly,
	}).Run(ctx, quiet(stdio), b.partition); err != nil {
		return err
	}

	fmt.Fprintln(stdio.Stderr, restartHint("the instance mounts volumes at boot"))
	return nil
}

type shellUnmountBuiltin struct {
	Volume string `arg:"" completion-predictor:"resource-key-volume" help:"Volume to detach."`
}

func (c shellUnmountBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	if err := (&VolumeDetachCmd{Volume: c.Volume, From: b.key}).Run(ctx, quiet(stdio), b.partition); err != nil {
		return err
	}

	fmt.Fprintln(stdio.Stderr, restartHint("the instance mounts volumes at boot"))
	return nil
}

type shellStartBuiltin struct{}

func (shellStartBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&InstancesStartCmd{Targets: []string{b.key}}).Run(ctx, stdio)
}

type shellStopBuiltin struct{}

func (shellStopBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&InstancesStopCmd{Targets: []string{b.key}}).Run(ctx, stdio)
}

type shellRestartBuiltin struct{}

func (shellRestartBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&InstancesRestartCmd{Targets: []string{b.key}}).Run(ctx, stdio)
}

type shellSuspendBuiltin struct{}

func (shellSuspendBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&InstancesSuspendCmd{Targets: []string{b.key}}).Run(ctx, stdio)
}

type shellHelpBuiltin struct{}

func (shellHelpBuiltin) Run(stdio config.Stdio, list builtinList) error {
	fmt.Fprintln(stdio.Stdout, "Builtins run on this CLI rather than the instance:")
	list(stdio.Stdout)
	return nil
}

func restartHint(what string) string {
	return restartHintStyle.Render(what + `; ":restart" for this to take effect`)
}

// quiet keeps a builtin's own report off the shell.
func quiet(stdio config.Stdio) config.Stdio {
	stdio.Stdout = io.Discard
	return stdio
}
