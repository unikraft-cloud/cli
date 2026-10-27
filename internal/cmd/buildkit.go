// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/buildkit"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/multimetro"
)

// The builder that the build service creates for a remote build.
const (
	builderName     = "buildkit-default"
	builderVCPUs    = 4
	builderMemoryMB = 4096
	builderCacheMB  = 20480
)

var errNoMetro = errors.New("the profile has no default metro: use --metro")

type BuildkitCmd struct {
	Create BuildkitCreateCmd `cmd:"" help:"Create the builder that remote builds run on."`
	Du     BuildkitDuCmd     `cmd:"" help:"Show the disk usage of the builder's cache."`
	Prune  BuildkitPruneCmd  `cmd:"" help:"Free the builder's cache, keeping the builder."`
}

type BuildkitCreateCmd struct {
	Metro string `help:"Metro to create the builder in, the build service's. Defaults to the profile's default metro." placeholder:"metro"`
}

func (BuildkitCreateCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Create the builder of remote builds in fra",
			Commands:    []string{"unikraft buildkit create --metro fra"},
		},
	}
}

// Run creates the build service's builder, unless it exists, which the build
// service wakes itself.
func (c *BuildkitCreateCmd) Run(ctx context.Context) error {
	// NewClient reports a bad profile, which defaultMetro hides.
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	metro := defaultMetro(ctx, c.Metro)
	if metro == "" {
		return errNoMetro
	}
	return group.DoMetro(ctx, g, metro, func(ctx context.Context, mc multimetro.MetroClient) error {
		p, exists, err := builder(ctx, mc)
		if err != nil {
			return err
		}
		if exists {
			log.G(ctx).Info().Str("name", builderName).Msg("builder exists")
			return nil
		}

		if err := p.Create(ctx, buildkit.Key{}); err != nil {
			return err
		}
		log.G(ctx).Info().Str("name", builderName).Msg("created builder")
		return nil
	})
}

type BuildkitDuCmd struct {
	Metro string `help:"Metro of the builder, the build service's. Defaults to the profile's default metro." placeholder:"metro"`
}

func (BuildkitDuCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Show how much cache the builder of remote builds in fra holds",
			Commands:    []string{"unikraft buildkit du --metro fra"},
		},
	}
}

type BuildkitPruneCmd struct {
	Metro string `help:"Metro of the builder, the build service's. Defaults to the profile's default metro." placeholder:"metro"`
}

func (BuildkitPruneCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Free the cache of the builder of remote builds in fra",
			Commands:    []string{"unikraft buildkit prune --metro fra"},
		},
	}
}

// builder returns the platform of the build service's builder in mc's metro,
// and whether that builder exists.
func builder(ctx context.Context, mc multimetro.MetroClient) (*buildkit.Platform, bool, error) {
	resp, err := mc.GetInstances(ctx, []platform.NameOrUUID{{Name: new(builderName)}}, platform.GetInstancesOpts{Details: new(false)})
	if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
		return nil, false, err
	}
	p := buildkit.NewPlatform(mc.Client,
		buildkit.WithVCPUs(builderVCPUs),
		buildkit.WithMemory(builderMemoryMB),
		buildkit.WithCacheSize(builderCacheMB),
		buildkit.WithInsecure(ptr.ZeroIfNil(mc.Metro.Insecure)),
	)
	return p, err == nil && resp.Data != nil && len(resp.Data.Instances) > 0, nil
}
