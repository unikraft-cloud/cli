// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/types"
)

// InstanceCommands runs the CLI's instance commands on behalf of the
// instance tools, writing their output to the given stdio.
type InstanceCommands interface {
	Start(ctx context.Context, stdio config.Stdio, instances []string) error
	Stop(ctx context.Context, stdio config.Stdio, instances []string, opts InstanceStopOptions) error
	Restart(ctx context.Context, stdio config.Stdio, instances []string, opts InstanceStopOptions) error
	Suspend(ctx context.Context, stdio config.Stdio, instances []string, drainTimeout types.DurationMS) error
	Logs(ctx context.Context, stdio config.Stdio, instance string, tail int) error
}

// InstanceStopOptions carries the flags shared by stop and restart; a
// negative DrainTimeout keeps the platform's default.
type InstanceStopOptions struct {
	Force        bool
	DrainTimeout types.DurationMS
}

type instanceControlArgs struct {
	Action         string   `json:"action" jsonschema:"What to do with the instances."`
	Instances      []string `json:"instances" jsonschema:"Instances to act on: names, UUIDs or metro/name keys."`
	Force          bool     `json:"force,omitzero" jsonschema:"For stop and restart: stop immediately instead of draining connections first."`
	DrainTimeoutMS *int64   `json:"drain_timeout_ms,omitzero" jsonschema:"For stop, restart and suspend: milliseconds to drain connections first. Defaults to the platform's timeout."`
}

type instanceLogsArgs struct {
	Instance string `json:"instance" jsonschema:"The instance to read logs from: a name, UUID or metro/name key."`
	Tail     int    `json:"tail,omitzero" jsonschema:"How many lines from the end of the log to return. Defaults to 200."`
}

const (
	instanceActionStart   = "start"
	instanceActionStop    = "stop"
	instanceActionRestart = "restart"
	instanceActionSuspend = "suspend"

	defaultLogTail = 200
)

const defaultDrainTimeout types.DurationMS = -1

func (s *Server) AddInstanceTools(commands InstanceCommands) error {
	errs := []error{
		s.addCommandTool(&mcp.Tool{
			Name:        "instance_logs",
			Description: "Read the most recent console output of an instance.",
			Annotations: readOnlyAnnotations(),
		}, false, func(ctx context.Context, in instanceLogsArgs, stdio config.Stdio) error {
			return commands.Logs(ctx, stdio, in.Instance, cmp.Or(in.Tail, defaultLogTail))
		}),
	}
	if s.opts.ReadOnly {
		return errors.Join(errs...)
	}

	schema, err := jsonschema.For[instanceControlArgs](nil)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	schema.Properties["action"].Enum = []any{instanceActionStart, instanceActionStop, instanceActionRestart, instanceActionSuspend}
	errs = append(errs, s.addCommandTool(&mcp.Tool{
		Name:        "instance_control",
		Description: "Start, stop, restart or suspend instances. Stop and restart drain connections first unless forced; a suspended instance resumes from memory on its next request. The updated instances are returned.",
		InputSchema: schema,
		Annotations: mutatingAnnotations(false),
	}, true, func(ctx context.Context, in instanceControlArgs, stdio config.Stdio) error {
		drain := drainTimeout(in.DrainTimeoutMS)
		switch in.Action {
		case instanceActionStart:
			return commands.Start(ctx, stdio, in.Instances)
		case instanceActionStop:
			return commands.Stop(ctx, stdio, in.Instances, InstanceStopOptions{Force: in.Force, DrainTimeout: drain})
		case instanceActionRestart:
			return commands.Restart(ctx, stdio, in.Instances, InstanceStopOptions{Force: in.Force, DrainTimeout: drain})
		case instanceActionSuspend:
			return commands.Suspend(ctx, stdio, in.Instances, drain)
		default:
			return fmt.Errorf("unknown action %q: use %s, %s, %s or %s", in.Action,
				instanceActionStart, instanceActionStop, instanceActionRestart, instanceActionSuspend)
		}
	}))
	return errors.Join(errs...)
}

func drainTimeout(ms *int64) types.DurationMS {
	if ms == nil {
		return defaultDrainTimeout
	}
	return types.DurationMS(*ms)
}
