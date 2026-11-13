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
	"strings"

	"unikraft.com/x/kingkong"

	"unikraft.com/cli/pkg/model"
	xstdio "unikraft.com/x/stdio"
)

// InstanceRunCmd is a convenience wrapper around `instance create` that adds
// log following capability. All instance creation flags are inherited from
// InstanceCreateCmd via embedding.
type InstanceRunCmd struct {
	InstanceCreateCmd

	// Follow is the only run-specific flag; it tails logs after creation.
	Follow bool `help:"Follow instance logs after creation, until the instances stop."`
}

func (InstanceRunCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Deploy a new instance and expose a HTTPS service",
			Commands: []string{
				"unikraft instance run --metro=sfo --image=nginx:latest -p 443:8080/http+tls",
			},
		},
		{
			Description: "Deploy a new instance and expose a HTTPS service and redirect from HTTP to HTTPS",
			Commands: []string{
				"unikraft instance run --metro=sfo --image=nginx:latest -p 443:8080/http+tls -p 80:443/http+redirect",
			},
		},
		{
			Description: "Deploy and tail logs from a new instance",
			Commands: []string{
				"unikraft instance run --metro=fra --image=nginx:latest --follow",
			},
		},
		{
			Description: "Preview instance creation without executing",
			Commands: []string{
				"unikraft instance run --metro=dal --image=nginx:latest --dry-run",
			},
		},
		{
			Description: "Deploy a new instance with environment variables",
			Commands: []string{
				"unikraft instance run --metro=was --image=my-app:latest -e KEY1=VALUE1 -e KEY2=VALUE2",
			},
		},
		{
			Description: "Deploy a new instance with attached volume",
			Commands: []string{
				"unikraft instance run --metro=sin --image=my-app:latest -v my-volume:/data",
			},
		},
		{
			Description: "Deploy a new instance with attached volume which is read-only",
			Commands: []string{
				"unikraft instance run --metro=sin --image=my-app:latest -v my-volume:/data:ro",
			},
		},
		{
			Description: "Deploy a new instance with custom resource allocations",
			Commands: []string{
				"unikraft instance run --metro=sfo --image=my-app:latest -m 512MiB --vcpus 2",
			},
		},
		{
			Description: "Deploy a new instance with scale-to-zero enabled",
			Commands: []string{
				"unikraft instance run --metro=fra --image=my-app:latest --scale-to-zero policy=on,cooldown-time=300",
			},
		},
		{
			Description: "Deploy a new instance with specific restart policy",
			Commands: []string{
				"unikraft instance run --metro=dal --image=my-app:latest --restart=on-failure",
			},
		},
		{
			Description: "Deploy a new instance with a configured plugin",
			Commands: []string{
				`unikraft instance run --metro=fra --image=my-app:latest --plugin 'name=sandbox,image=plugins/sandbox:latest,config={"persist_path":"/data"}'`,
			},
		},
		{
			Description: "Roll every instance in a service group over to a new image",
			Commands: []string{
				"unikraft instance run --image=my-app:v2 --service=api --rollout",
			},
		},
	}
}

func (c *InstanceRunCmd) Run(ctx context.Context, stdio xstdio.Stdio, partition *model.Partition) error {
	// Unlike create, run starts the instance unless told otherwise.
	if !c.GeneratedFlags().IsSet("autostart") {
		c.Set = append(c.Set, map[string]string{"autostart": "true"})
	}
	created, err := c.RunModels(ctx, stdio, partition)
	if err != nil {
		return err
	}
	if !c.Follow || len(created) == 0 {
		return nil
	}

	fmt.Fprintln(stdio.Stdout)

	mux, watch, cancel, err := newInstanceLogMux(ctx, instanceKeys(created), nil, true, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer mux.Close()

	go watch()
	_, err = io.Copy(stdio.Stdout, mux)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// RunCmd is a top-level alias for `instance run`.
type RunCmd struct {
	InstanceRunCmd `embed:""`
}

func (RunCmd) Examples() []kingkong.Example {
	instanceExamples := InstanceRunCmd{}.Examples()
	for i := range instanceExamples {
		for j := range instanceExamples[i].Commands {
			instanceExamples[i].Commands[j] = strings.Replace(
				instanceExamples[i].Commands[j],
				"unikraft instance run",
				"unikraft run",
				1,
			)
		}
	}

	return instanceExamples
}

func (c *RunCmd) Run(ctx context.Context, stdio xstdio.Stdio, partition *model.Partition) error {
	return c.InstanceRunCmd.Run(ctx, stdio, partition)
}
