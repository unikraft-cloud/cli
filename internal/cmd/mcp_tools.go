// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcpserver"
	"unikraft.com/cli/internal/types"
)

// mcpInstanceCommands runs the MCP instance tools with the CLI's own commands.
type mcpInstanceCommands struct{}

var _ mcpserver.InstanceCommands = mcpInstanceCommands{}

func (mcpInstanceCommands) Start(ctx context.Context, stdio config.Stdio, instances []string) error {
	return (&InstancesStartCmd{Targets: instances, FormatOpts: mcpserver.JSONFormat()}).Run(ctx, stdio)
}

func (mcpInstanceCommands) Stop(ctx context.Context, stdio config.Stdio, instances []string, opts mcpserver.InstanceStopOptions) error {
	return (&InstancesStopCmd{
		Targets:      instances,
		Force:        opts.Force,
		DrainTimeout: opts.DrainTimeout,
		FormatOpts:   mcpserver.JSONFormat(),
	}).Run(ctx, stdio)
}

func (mcpInstanceCommands) Restart(ctx context.Context, stdio config.Stdio, instances []string, opts mcpserver.InstanceStopOptions) error {
	return (&InstancesRestartCmd{
		Targets:      instances,
		Force:        opts.Force,
		DrainTimeout: opts.DrainTimeout,
		FormatOpts:   mcpserver.JSONFormat(),
	}).Run(ctx, stdio)
}

func (mcpInstanceCommands) Suspend(ctx context.Context, stdio config.Stdio, instances []string, drainTimeout types.DurationMS) error {
	return (&InstancesSuspendCmd{
		Targets:      instances,
		DrainTimeout: drainTimeout,
		FormatOpts:   mcpserver.JSONFormat(),
	}).Run(ctx, stdio)
}

func (mcpInstanceCommands) Logs(ctx context.Context, stdio config.Stdio, instance string, tail int) error {
	return (&InstancesLogsCmd{Targets: []string{instance}, Tail: new(tail)}).Run(ctx, stdio)
}
