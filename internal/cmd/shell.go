// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"io"

	"unikraft.com/x/kingkong"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/shell"
)

type ShellSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to open a shell on."`

	Plugin string            `name:"plugin" help:"Plugin name from the instance to run commands on."`
	Dir    string            `name:"dir" help:"Initial working directory"`
	Env    map[string]string `name:"env" help:"Initial environment variables to set (KEY=VALUE)" mapsep:","`
}

func (cmd ShellSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Open an interactive shell session to a sandbox instance",
			Commands: []string{
				"unikraft instance shell my-instance",
			},
		},
	}
}

// Run hands the shell what it cannot reach on its own: the resources its
// builtins read and edit, an exec transport, and the lifecycle commands. The
// shell works against the resource interfaces, the way `unikraft tui` does.
func (c *ShellSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin, allowStopped)
	if err != nil {
		return err
	}

	registry := resource.NewRegistry()
	registry.Register(sandbox.WrapListable(Instance{}), sandbox.WrapEditable(Instance{}))
	registry.Register(sandbox.WrapListable(Volume{}), sandbox.WrapCreatable(Volume{}))

	g, key, plugin := target.g, target.key, target.plugin

	return shell.Run(ctx, shell.Config{
		Registry: registry,
		Group:    g,
		Key:      key,
		Plugin:   plugin,
		Running:  requireRunningInstance(target.instance) == nil,
		Dir:      c.Dir,
		Env:      c.Env,
		Exec: func(ctx context.Context, out io.Writer, in io.Reader, dir string, env map[string]string, line string) error {
			return execSandboxInstance(ctx, out, in, g, key, ExecOpts{
				Cmd:    []string{line},
				Dir:    dir,
				Env:    env,
				Plugin: plugin,
				Raw:    true,
			})
		},
		Lifecycle: shell.Lifecycle{
			Start: func(ctx context.Context, stdio config.Stdio) error {
				cmd := InstancesStartCmd{Targets: []string{key.String()}}
				return cmd.Run(ctx, stdio)
			},
			Stop: func(ctx context.Context, stdio config.Stdio) error {
				cmd := InstancesStopCmd{
					Targets:  []string{key.String()},
					StopOpts: StopOpts{DrainTimeout: -1},
				}
				return cmd.Run(ctx, stdio)
			},
			Suspend: func(ctx context.Context, stdio config.Stdio) error {
				cmd := InstancesSuspendCmd{Targets: []string{key.String()}, DrainTimeout: -1}
				return cmd.Run(ctx, stdio)
			},
			Restart: func(ctx context.Context, stdio config.Stdio) error {
				cmd := InstancesRestartCmd{
					Targets:  []string{key.String()},
					StopOpts: StopOpts{DrainTimeout: -1},
				}
				return cmd.Run(ctx, stdio)
			},
		},
	}, stdio)
}
