// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package shellbuiltins answers an instance shell's ":" commands with the CLI's own commands.
package shellbuiltins

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"unikraft.com/cloud/sdk/plugins/sandbox"

	"unikraft.com/x/colors"
	"unikraft.com/x/shell"
	xstdio "unikraft.com/x/stdio"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/pkg/types"
)

// instanceReadyTimeout is an arbitrary allowance for ":start" and ":restart" to wait for the instance to answer.
const instanceReadyTimeout = 90 * time.Second

// Instance is the instance the builtins act on.
type Instance interface {
	Get(ctx context.Context, stdio xstdio.Stdio, format cmd.FormatOpts) error
	Volumes(ctx context.Context, stdio xstdio.Stdio, format cmd.FormatOpts) error
	Edit(ctx context.Context, stdio xstdio.Stdio, set map[string]string) error
	Attach(ctx context.Context, stdio xstdio.Stdio, volume, at string, readonly bool) error
	Detach(ctx context.Context, stdio xstdio.Stdio, volume string) error
	Start(ctx context.Context, stdio xstdio.Stdio) error
	Stop(ctx context.Context, stdio xstdio.Stdio, opts StopOpts) error
	Restart(ctx context.Context, stdio xstdio.Stdio, opts StopOpts) error
	Suspend(ctx context.Context, stdio xstdio.Stdio, drainTimeout types.DurationMS) error
}

type StopOpts struct {
	Force        bool             `help:"Force stop the instance immediately."`
	DrainTimeout types.DurationMS `help:"Timeout in milliseconds for draining connections before stopping." default:"-1"`
}

type Credentials struct {
	Token    string
	Metro    string
	Endpoint string
	Insecure bool
}

type shellBuiltins struct {
	inst   Instance
	target sandbox.Target
}

type builtinList func(io.Writer)

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

type shellVolumesBuiltin struct {
	cmd.FormatOpts
}

type shellEditBuiltin struct {
	Fields []string `arg:"" name:"field=value" help:"Fields to set on this instance."`
}

type shellMountBuiltin struct {
	Volume   string `arg:"" completion-predictor:"resource-key-volume" help:"Volume to attach."`
	At       string `arg:"" name:"path" help:"Absolute mount path inside the instance."`
	Readonly bool   `help:"Mount the volume as read-only."`
}

type shellUnmountBuiltin struct {
	Volume string `arg:"" completion-predictor:"resource-key-volume" help:"Volume to detach."`
}

type shellStartBuiltin struct{}

type shellStopBuiltin struct {
	StopOpts
}

type shellRestartBuiltin struct {
	StopOpts
}

type shellSuspendBuiltin struct {
	DrainTimeout types.DurationMS `help:"Timeout in milliseconds for draining connections before suspending." default:"-1"`
}

type shellHelpBuiltin struct{}

func New(inst Instance, target sandbox.Target) (map[string]shell.Builtin, error) {
	answers, err := newShellBuiltins(shellBuiltins{inst: inst, target: target})
	if err != nil {
		return nil, err
	}
	return answers.Builtins(), nil
}

// WithCredentials puts a config made of creds alone on ctx. It is for outside programs and nothing in the CLI calls it.
func WithCredentials(ctx context.Context, creds Credentials) context.Context {
	profile := config.Profile{Name: config.DefaultProfile, Token: creds.Token}
	if creds.Metro != "" || creds.Endpoint != "" {
		metro := config.MetroFrom(cmp.Or(creds.Endpoint, creds.Metro))
		metro.Name = cmp.Or(creds.Metro, metro.Name)
		metro.Insecure = new(creds.Insecure)
		profile.Metros = []config.Metro{metro}
	}
	return config.WithConfig(ctx, &config.Config{
		Profiles: map[string]config.Profile{config.DefaultProfile: profile},
	})
}

func (b shellBuiltins) awaitReady(ctx context.Context, stdio xstdio.Stdio) error {
	if b.target.Client == nil {
		return nil
	}
	fmt.Fprintln(stdio.Stderr, colors.WarningFg("waiting for the instance to answer"))
	return b.target.Client.WaitReady(ctx, b.target.Instance, instanceReadyTimeout, b.target.Opts...)
}

func newShellBuiltins(b shellBuiltins) (*Kong, error) {
	var answers *Kong

	answers, err := NewKong(KongConfig{
		Commands: func() any { return &shellBuiltinCmds{} },
		Options:  []kong.Option{kong.Description("Builtins answered here rather than on the instance.")},
		Bind: func(_ context.Context, streams xstdio.Stdio) []any {
			return []any{streams, b, builtinList(answers.List)}
		},
	})
	return answers, err
}

func (c shellGetBuiltin) Run(ctx context.Context, stdio xstdio.Stdio, b shellBuiltins) error {
	return b.inst.Get(ctx, stdio, c.FormatOpts)
}

func (c shellVolumesBuiltin) Run(ctx context.Context, stdio xstdio.Stdio, b shellBuiltins) error {
	return b.inst.Volumes(ctx, stdio, c.FormatOpts)
}

func (c shellEditBuiltin) Run(ctx context.Context, stdio xstdio.Stdio, b shellBuiltins) error {
	set := map[string]string{}
	for _, field := range c.Fields {
		name, value, ok := strings.Cut(field, "=")
		if !ok {
			return fmt.Errorf("%q is not <field>=<value>", field)
		}
		set[name] = value
	}

	if err := b.inst.Edit(ctx, quiet(stdio), set); err != nil {
		return err
	}

	fmt.Fprintln(stdio.Stderr, restartHint("the instance reads its settings at boot"))
	return nil
}

func (c shellMountBuiltin) Run(ctx context.Context, stdio xstdio.Stdio, b shellBuiltins) error {
	if err := b.inst.Attach(ctx, quiet(stdio), c.Volume, c.At, c.Readonly); err != nil {
		return err
	}

	fmt.Fprintln(stdio.Stderr, restartHint("the instance mounts volumes at boot"))
	return nil
}

func (c shellUnmountBuiltin) Run(ctx context.Context, stdio xstdio.Stdio, b shellBuiltins) error {
	if err := b.inst.Detach(ctx, quiet(stdio), c.Volume); err != nil {
		return err
	}

	fmt.Fprintln(stdio.Stderr, restartHint("the instance mounts volumes at boot"))
	return nil
}

func (shellStartBuiltin) Run(ctx context.Context, stdio xstdio.Stdio, b shellBuiltins) error {
	if err := b.inst.Start(ctx, stdio); err != nil {
		return err
	}
	return b.awaitReady(ctx, stdio)
}

func (c shellStopBuiltin) Run(ctx context.Context, stdio xstdio.Stdio, b shellBuiltins) error {
	return b.inst.Stop(ctx, stdio, c.StopOpts)
}

func (c shellRestartBuiltin) Run(ctx context.Context, stdio xstdio.Stdio, b shellBuiltins) error {
	if err := b.inst.Restart(ctx, stdio, c.StopOpts); err != nil {
		return err
	}
	return b.awaitReady(ctx, stdio)
}

func (c shellSuspendBuiltin) Run(ctx context.Context, stdio xstdio.Stdio, b shellBuiltins) error {
	return b.inst.Suspend(ctx, stdio, c.DrainTimeout)
}

func (shellHelpBuiltin) Run(stdio xstdio.Stdio, list builtinList) error {
	fmt.Fprintln(stdio.Stdout, "Builtins answered here rather than on the instance:")
	list(stdio.Stdout)
	return nil
}

func restartHint(what string) string {
	return colors.WarningFg(what + `; ":restart" for this to take effect`)
}

// quiet keeps a builtin's own report off the shell.
func quiet(stdio xstdio.Stdio) xstdio.Stdio {
	stdio.Stdout = io.Discard
	return stdio
}
