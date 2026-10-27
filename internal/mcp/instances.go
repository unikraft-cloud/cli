// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcp

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/cli/internal/config"
	modelmcp "unikraft.com/cli/internal/model/mcp"
	"unikraft.com/cli/internal/types"
)

const (
	defaultLogTail                       = 200
	defaultDrainTimeout types.DurationMS = -1
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
	Keys           []string `json:"keys" jsonschema:"Instances to act on: names or UUIDs, optionally prefixed with metro/."`
	Force          bool     `json:"force,omitzero" jsonschema:"For stop and restart: stop immediately instead of draining connections first."`
	DrainTimeoutMS *int64   `json:"drain_timeout_ms,omitzero" jsonschema:"For stop, restart and suspend: milliseconds to drain connections first. Defaults to the platform's timeout."`
}

type instanceLogsArgs struct {
	Key  string `json:"key" jsonschema:"The instance to read logs from: a name or UUID, optionally prefixed with metro/."`
	Tail int    `json:"tail,omitzero" jsonschema:"How many lines from the end of the log to return. Defaults to 200."`
}

func AddInstanceTools(tools *modelmcp.Tools, commands InstanceCommands) error {
	schema, err := jsonschema.For[instanceControlArgs](nil)
	if err != nil {
		return err
	}
	schema.Properties["action"].Enum = []any{"start", "stop", "restart", "suspend"}
	return errors.Join(tools.AddTool("instance", &mcp.Tool{
		Name:        "instance_logs",
		Title:       "Instance logs",
		Description: "Read the last lines of an instance's console output; the first place to look when an instance fails to start or misbehaves. Returns plain text.",
		Annotations: modelmcp.ReadOnlyAnnotations(),
	}, false, func(ctx context.Context, in instanceLogsArgs, stdio config.Stdio) error {
		return commands.Logs(ctx, stdio, in.Key, cmp.Or(in.Tail, defaultLogTail))
	}), tools.AddTool("instance", &mcp.Tool{
		Name:        "instance_control",
		Title:       "Control instances",
		Description: "Start, stop, restart or suspend instances. Stop and restart drain connections first unless force is set; a suspended instance resumes from memory on its next request. Returns the updated instances.",
		InputSchema: schema,
		Annotations: modelmcp.MutatingAnnotations(),
	}, true, func(ctx context.Context, in instanceControlArgs, stdio config.Stdio) error {
		if len(in.Keys) == 0 {
			return errors.New("no instances specified")
		}
		drain := defaultDrainTimeout
		if in.DrainTimeoutMS != nil {
			drain = types.DurationMS(*in.DrainTimeoutMS)
		}
		switch in.Action {
		case "start":
			return commands.Start(ctx, stdio, in.Keys)
		case "stop":
			return commands.Stop(ctx, stdio, in.Keys, InstanceStopOptions{Force: in.Force, DrainTimeout: drain})
		case "restart":
			return commands.Restart(ctx, stdio, in.Keys, InstanceStopOptions{Force: in.Force, DrainTimeout: drain})
		case "suspend":
			return commands.Suspend(ctx, stdio, in.Keys, drain)
		default:
			return fmt.Errorf("unknown action %q: use start, stop, restart or suspend", in.Action)
		}
	}))
}
