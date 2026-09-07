// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"fmt"

	"unikraft.com/cli/internal/builder"
	"unikraft.com/cli/internal/builder/buildflags"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/resource"
	imagespec "unikraft.com/x/image-spec"
)

func (c *ImageBuildCmd) Run(ctx context.Context, cfg *config.Config, partition *resource.Partition) error {
	buildOpts, err := builder.LoadBuildOpts(ctx, c.Input)
	if err != nil {
		return err
	}

	arches := make([]string, 0, len(c.Arch))
	for _, a := range c.Arch {
		arch, err := imagespec.NormalizeArch(a)
		if err != nil {
			return fmt.Errorf("%w: expected %s or %s", err, imagespec.ArchitectureIntel, imagespec.ArchitectureArm)
		}
		arches = append(arches, arch)
	}
	if err := buildOpts.FilterArch(arches...); err != nil {
		return err
	}
	if buildOpts.Runtime == "" && len(buildOpts.Platform) == 0 {
		return fmt.Errorf("no target architecture: use --arch, or declare a target or runtime in a Kraftfile")
	}

	buildOpts.NoCache = c.NoCache
	if len(c.BuildArg) > 0 {
		buildOpts.BuildArg = append(buildOpts.BuildArg, c.BuildArg...)
	}
	if len(c.Secret) > 0 {
		secrets, err := buildflags.ParseSecretSpecs(c.Secret)
		if err != nil {
			return err
		}
		buildOpts.Secrets = secrets
	}
	if len(c.SSH) > 0 {
		ssh, err := buildflags.ParseSSHSpecs(c.SSH)
		if err != nil {
			return err
		}
		buildOpts.SSH = ssh
	}

	imgs, err := builder.Build(ctx, buildOpts)
	if err != nil {
		return err
	}
	defer func() {
		for _, img := range imgs {
			img.Close()
		}
	}()

	if c.Output == "" {
		return nil
	}
	output, err := imagespec.GuessURI(c.Output)
	if err != nil {
		return err
	}

	var opts []images.AccessorOpt
	if c.Insecure != nil {
		if len(c.Insecure) > 0 {
			opts = append(opts, images.WithInsecureRegistry(c.Insecure...))
		} else {
			opts = append(opts, images.WithInsecureRegistries())
		}
	}

	access, err := images.Accessor(ctx, opts...)
	if err != nil {
		return err
	}
	err = access.Save(ctx, output, imgs...)
	if err != nil {
		return err
	}

	if partition != nil && output.Scheme == imagespec.URISchemeOCI {
		if err := addImageToPartition(ctx, partition, output.Path); err != nil {
			return fmt.Errorf("adding built image to partition: %w", err)
		}
	}

	return nil
}

func (c *BuildCmd) Run(ctx context.Context, cfg *config.Config, partition *resource.Partition) error {
	return c.ImageBuildCmd.Run(ctx, cfg, partition)
}
