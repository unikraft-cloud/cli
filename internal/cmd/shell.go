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
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/MakeNowJust/heredoc"
	"github.com/alecthomas/kong"

	"unikraft.com/x/colors"
	"unikraft.com/x/kingkong"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/shell"
	xsignal "unikraft.com/cli/internal/x/signal"
)

var restartHintStyle = lipgloss.NewStyle().Foreground(colors.Warning)

// restartingBuiltins take the instance down and up again, so the shell waits for it.
var restartingBuiltins = []string{"restart", "start"}

// shellBuiltinNodes is the grammar as kong sees it, rather than a list kept alongside.
var shellBuiltinNodes = sync.OnceValue(func() []*kong.Node {
	parser, err := newShellBuiltinParser(&shellBuiltinCmds{}, config.Stdio{Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		panic(err)
	}

	nodes := slices.Clone(parser.Model.Children)
	slices.SortFunc(nodes, func(a, b *kong.Node) int { return strings.Compare(a.Name, b.Name) })
	return nodes
})

type ShellSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to open a shell on."`

	Plugin  string            `name:"plugin" default:"${sandbox_plugin}" help:"Name of the sandbox plugin to use." placeholder:"name"`
	Dir     string            `name:"dir" help:"Directory to start the shell in." default:"/"`
	Env     map[string]string `name:"env" short:"e" help:"Environment variables." placeholder:"<key>=<value>" example:"DEBUG=true,PORT=8080" mapsep:","`
	Command string            `name:"command" short:"c" help:"Run a single command line and exit."`
}

func (ShellSandboxInstanceCmd) Help() string {
	return heredoc.Docf(`
		The shell itself runs on your machine and interprets what you type, so
		every command lands in one of two places:

		  %[1]s:%[1]s prefixed   a builtin, answered by the CLI — %[1]s:help%[1]s lists them
		  anything else  runs on the instance

		Session state — the working directory, variables, functions, %[1]s$?%[1]s — is
		kept here, and so is everything the shell language does: pipelines,
		redirections, globs and control flow. Paths, though, resolve against the
		instance, so %[1]scd%[1]s, %[1]s*.log%[1]s and %[1]s> file%[1]s all mean what you would expect.

		While a command runs it is the one reading your keyboard, so prompts like
		%[1]sDo you want to continue? [Y/n]%[1]s can be answered — and anything typed ahead
		goes to that command rather than to the next prompt.

		Ctrl-C interrupts the command on the instance, and the shell waits for it
		to stop before reporting what it stopped of in %[1]s$?%[1]s; the rest of the line
		is abandoned.

		The instance offers no terminal, so programs that need one — %[1]svim%[1]s, %[1]stop%[1]s,
		%[1]sless%[1]s — will not work there yet.
		A command that fails on the instance sets %[1]s$?%[1]s but does not fail the shell.
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

func (c *ShellSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	target, err := resolveSandboxTarget(ctx, partition, c.Target, c.Plugin)
	if err != nil {
		return err
	}
	return shell.Run(ctx, shell.Config{
		Instance:       c.Target,
		Dir:            c.Dir,
		Env:            c.Env,
		Command:        c.Command,
		Transport:      sandboxTransport{target: target},
		Builtins:       shellBuiltins{key: c.Target, partition: partition},
		SuspendSignals: xsignal.Suspend,
	}, shell.Streams{In: stdio.Stdin, Out: stdio.Stdout, Err: stdio.Stderr})
}

// shellBuiltins answers the lines opening with ":".
type shellBuiltins struct {
	key       string
	partition *resource.Partition
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

func (shellHelpBuiltin) Run(stdio config.Stdio) error {
	fmt.Fprintln(stdio.Stdout, "Builtins run on this CLI rather than the instance:")
	for _, node := range shellBuiltinNodes() {
		fmt.Fprintf(stdio.Stdout, "  %-34s %s\n", node.Summary(), node.Help)
	}
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

func (b shellBuiltins) Restarts(args []string) bool {
	return len(args) > 0 && slices.Contains(restartingBuiltins, args[0])
}

func newShellBuiltinParser(cmds *shellBuiltinCmds, stdio config.Stdio) (*kong.Kong, error) {
	return kong.New(cmds,
		kong.Name(""),
		kong.Description("Builtins run on this CLI rather than the instance."),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true, FlagsLast: true}),
		kong.Writers(stdio.Stdout, stdio.Stderr),
		kong.Exit(func(int) {}),
		sandboxKongVars,
	)
}

func (b shellBuiltins) Names() []string {
	names := make([]string, 0, len(shellBuiltinNodes()))
	for _, node := range shellBuiltinNodes() {
		names = append(names, strings.TrimPrefix(node.Name, shell.BuiltinSigil))
	}
	return names
}

func (b shellBuiltins) Run(ctx context.Context, streams shell.Streams, args []string) (int, error) {
	stdio := config.Stdio{Stdin: streams.In, Stdout: streams.Out, Stderr: streams.Err}

	if !slices.Contains(b.Names(), args[0]) {
		return 0, fmt.Errorf("unknown builtin %q; try %q", args[0], shell.BuiltinSigil+"help")
	}

	args = append([]string{shell.BuiltinSigil + args[0]}, args[1:]...)

	parser, err := newShellBuiltinParser(&shellBuiltinCmds{}, stdio)
	if err != nil {
		return 0, err
	}

	kctx, err := parser.Parse(args)
	if err != nil {
		var parseErr *kong.ParseError
		if errors.As(err, &parseErr) && helpAsked(parseErr.Context) {
			return 0, nil
		}
		return 1, err
	}
	if helpAsked(kctx) {
		return 0, nil
	}

	kctx.BindTo(ctx, (*context.Context)(nil))
	kctx.Bind(stdio, b)

	if err := kctx.Run(); err != nil {
		return 1, err
	}
	return 0, nil
}

func helpAsked(kctx *kong.Context) bool {
	for _, flag := range kctx.Flags() {
		if flag.Name == "help" {
			return flag.Set
		}
	}
	return false
}
