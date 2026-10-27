// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"path"
	"strings"

	"unikraft.com/cloud/sdk/plugins/sandbox"
	"unikraft.com/x/shell"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcp"
	"unikraft.com/cli/internal/model"
	modelmcp "unikraft.com/cli/internal/model/mcp"
	"unikraft.com/cli/internal/types"
)

// mcpInstanceCommands runs the MCP instance tools with the CLI's own commands.
type mcpInstanceCommands struct{}

func (mcpInstanceCommands) Start(ctx context.Context, stdio config.Stdio, instances []string) error {
	return (&InstancesStartCmd{Targets: instances, FormatOpts: modelmcp.JSONFormat()}).Run(ctx, stdio)
}

func (mcpInstanceCommands) Stop(ctx context.Context, stdio config.Stdio, instances []string, opts mcp.InstanceStopOptions) error {
	return (&InstancesStopCmd{
		Targets:      instances,
		Force:        opts.Force,
		DrainTimeout: opts.DrainTimeout,
		FormatOpts:   modelmcp.JSONFormat(),
	}).Run(ctx, stdio)
}

func (mcpInstanceCommands) Restart(ctx context.Context, stdio config.Stdio, instances []string, opts mcp.InstanceStopOptions) error {
	return (&InstancesRestartCmd{
		Targets:      instances,
		Force:        opts.Force,
		DrainTimeout: opts.DrainTimeout,
		FormatOpts:   modelmcp.JSONFormat(),
	}).Run(ctx, stdio)
}

func (mcpInstanceCommands) Suspend(ctx context.Context, stdio config.Stdio, instances []string, drainTimeout types.DurationMS) error {
	return (&InstancesSuspendCmd{
		Targets:      instances,
		DrainTimeout: drainTimeout,
		FormatOpts:   modelmcp.JSONFormat(),
	}).Run(ctx, stdio)
}

func (mcpInstanceCommands) Logs(ctx context.Context, stdio config.Stdio, instance string, tail int) error {
	return (&InstancesLogsCmd{Targets: []string{instance}, Tail: new(tail)}).Run(ctx, stdio)
}

type mcpSandboxCommands struct {
	partition *model.Partition
}

func (c mcpSandboxCommands) Exec(ctx context.Context, stdio config.Stdio, instance string, opts mcp.SandboxExecOptions) (int, error) {
	target, err := c.target(ctx, instance)
	if err != nil {
		return 0, err
	}
	var run *sandbox.Cmd
	if opts.Cmdline != "" {
		run = target.CommandLine(ctx, opts.Cmdline)
	} else {
		run = target.CommandArgs(ctx, opts.Args)
	}
	run.Dir = opts.Dir
	run.Env = opts.Env
	run.Stdin = strings.NewReader(opts.Stdin)
	run.Stdout = stdio.Stdout
	run.Stderr = stdio.Stdout
	return shell.ExitStatus(run.Run())
}

func (c mcpSandboxCommands) ReadFile(ctx context.Context, instance, remote string) ([]byte, error) {
	target, err := c.target(ctx, instance)
	if err != nil {
		return nil, err
	}
	return target.ReadFile(ctx, remote)
}

func (c mcpSandboxCommands) WriteFile(ctx context.Context, instance, remote string, data []byte, opts mcp.SandboxWriteOptions) error {
	target, err := c.target(ctx, instance)
	if err != nil {
		return err
	}
	if opts.Parents {
		if err := target.Mkdir(ctx, path.Dir(remote), true); err != nil {
			return err
		}
	}
	return target.WriteFile(ctx, remote, data, opts.Append)
}

func (c mcpSandboxCommands) target(ctx context.Context, instance string) (sandbox.Target, error) {
	inst, err := lookupInstance(ctx, c.partition, instance)
	if err != nil {
		return sandbox.Target{}, err
	}
	plugin := sandbox.PluginName
	if cause := missingPlugin(inst, plugin); cause != nil {
		if _, ok := pendingPlugin(inst, plugin); ok {
			return sandbox.Target{}, fmt.Errorf("instance %q has plugin %q attached but not loaded yet; restart it with instance_control", inst.Name, plugin)
		}
		return sandbox.Target{}, fmt.Errorf("%w; attach it with instance_edit by adding plugins name=%s,image=%s, then restart the instance with instance_control", cause, plugin, defaultPluginImage)
	}
	if !inst.State.IsRunning() {
		return sandbox.Target{}, fmt.Errorf("instance %q is not running (state: %s); start it with instance_control", inst.Name, string(inst.State))
	}
	return sandboxTargetFor(ctx, inst, plugin)
}
