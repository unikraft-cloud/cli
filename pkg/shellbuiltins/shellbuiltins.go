// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package shellbuiltins answers an instance shell's ":" commands with the CLI's own commands.
package shellbuiltins

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"unikraft.com/cloud/sdk/plugins/sandbox"

	"unikraft.com/x/colors"
	"unikraft.com/x/shell"
	"unikraft.com/x/shell/builtins"
	xstdio "unikraft.com/x/stdio"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	rcmd "unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
)

// instanceReadyTimeout is an arbitrary allowance for ":start" and ":restart" to wait for the instance to answer.
const instanceReadyTimeout = 90 * time.Second

func New(instance string, target sandbox.Target) (map[string]shell.Builtin, error) {
	answers, err := newShellBuiltins(shellBuiltins{key: instance, target: target})
	if err != nil {
		return nil, err
	}
	return answers.Builtins(), nil
}

type Credentials struct {
	Token    string
	Metro    string
	Insecure bool
}

// WithCredentials puts a config made of creds alone on ctx. It is for outside programs and nothing in the CLI calls it.
func WithCredentials(ctx context.Context, creds Credentials) context.Context {
	profile := config.Profile{Name: config.DefaultProfile, Token: creds.Token}
	if creds.Metro != "" {
		metro := config.MetroFrom(creds.Metro)
		metro.Insecure = new(creds.Insecure)
		profile.Metros = []config.Metro{metro}
	}
	return config.WithConfig(ctx, &config.Config{
		Profiles: map[string]config.Profile{config.DefaultProfile: profile},
	})
}

type shellBuiltins struct {
	key    string
	target sandbox.Target
}

func (b shellBuiltins) awaitReady(ctx context.Context, stdio config.Stdio) error {
	if b.target.Client == nil {
		return nil
	}
	fmt.Fprintln(stdio.Stderr, colors.WarningFg("waiting for the instance to answer"))
	return b.target.Client.WaitReady(ctx, b.target.Instance, instanceReadyTimeout, b.target.Opts...)
}

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
	rcmd.FormatOpts
}

func (c shellGetBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&rcmd.ResourceGetCmd[cmd.Instance]{
		Targets:    []string{b.key},
		FormatOpts: c.FormatOpts,
	}).Run(ctx, stdio, resource.PartitionFromContext(ctx))
}

type shellVolumesBuiltin struct {
	rcmd.FormatOpts
}

func (c shellVolumesBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&rcmd.ResourceListCmd[cmd.Volume]{FormatOpts: c.FormatOpts}).Run(ctx, stdio, resource.PartitionFromContext(ctx))
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

	if err := (&rcmd.ResourceEditCmd[cmd.Instance]{
		Target: b.key,
		Set:    []map[string]string{set},
	}).Run(ctx, quiet(stdio), resource.PartitionFromContext(ctx)); err != nil {
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
	if err := (&cmd.VolumeAttachCmd{
		Volume:   c.Volume,
		To:       b.key,
		At:       c.At,
		Readonly: c.Readonly,
	}).Run(ctx, quiet(stdio), resource.PartitionFromContext(ctx)); err != nil {
		return err
	}

	fmt.Fprintln(stdio.Stderr, restartHint("the instance mounts volumes at boot"))
	return nil
}

type shellUnmountBuiltin struct {
	Volume string `arg:"" completion-predictor:"resource-key-volume" help:"Volume to detach."`
}

func (c shellUnmountBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	if err := (&cmd.VolumeDetachCmd{Volume: c.Volume, From: b.key}).Run(ctx, quiet(stdio), resource.PartitionFromContext(ctx)); err != nil {
		return err
	}

	fmt.Fprintln(stdio.Stderr, restartHint("the instance mounts volumes at boot"))
	return nil
}

type shellStartBuiltin struct{}

func (shellStartBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	if err := (&cmd.InstancesStartCmd{Targets: []string{b.key}}).Run(ctx, stdio); err != nil {
		return err
	}
	return b.awaitReady(ctx, stdio)
}

type shellStopBuiltin struct {
	cmd.StopOpts
}

func (c shellStopBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&cmd.InstancesStopCmd{Targets: []string{b.key}, StopOpts: c.StopOpts}).Run(ctx, stdio)
}

type shellRestartBuiltin struct {
	cmd.StopOpts
}

func (c shellRestartBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	if err := (&cmd.InstancesRestartCmd{Targets: []string{b.key}, StopOpts: c.StopOpts}).Run(ctx, stdio); err != nil {
		return err
	}
	return b.awaitReady(ctx, stdio)
}

type shellSuspendBuiltin struct {
	DrainTimeout types.DurationMS `help:"Timeout in milliseconds for draining connections before suspending." default:"-1"`
}

func (c shellSuspendBuiltin) Run(ctx context.Context, stdio config.Stdio, b shellBuiltins) error {
	return (&cmd.InstancesSuspendCmd{Targets: []string{b.key}, DrainTimeout: c.DrainTimeout}).Run(ctx, stdio)
}

type shellHelpBuiltin struct{}

func (shellHelpBuiltin) Run(stdio config.Stdio, list builtinList) error {
	fmt.Fprintln(stdio.Stdout, "Builtins run on this CLI rather than the instance:")
	list(stdio.Stdout)
	return nil
}

func restartHint(what string) string {
	return colors.WarningFg(what + `; ":restart" for this to take effect`)
}

// quiet keeps a builtin's own report off the shell.
func quiet(stdio config.Stdio) config.Stdio {
	stdio.Stdout = io.Discard
	return stdio
}
