// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"strings"

	"unikraft.com/cli/internal/builder"
	"unikraft.com/cli/internal/builder/buildflags"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/resource"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/kingkong"
)

type ImageBuildCmd struct {
	Input  string   `arg:"" default:"." help:"Path to the project directory, Kraftfile, or Dockerfile. A directory without a Kraftfile is built from its Dockerfile."`
	Output string   `short:"o" help:"Output destination"`
	Arch   []string `help:"Only build the Kraftfile targets of these architectures. Defaults to every declared target. Required when the project declares no targets and no runtime, such as a bare Dockerfile." example:"x86_64,arm64"`

	// similar to docker compose build
	BuildArg []string `sep:"none" help:"Set build-time variables."`
	NoCache  bool     `help:"Do not use cache when building the image."`
	Secret   []string `sep:"none" help:"Secret to expose to the build (format: \"id=mysecret[,src=/local/secret]\")."`
	SSH      []string `sep:"none" help:"SSH agent socket or keys to expose to the build (format: \"default|<id>[=<socket>|<key>[,<key>]]\")."`

	Insecure []string `help:"Allow insecure (HTTP/unverified TLS) connections to registries. Specify hostnames to restrict, or omit to apply to all." type:"optional"`
}

func (ImageBuildCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Build the project in the current directory",
			Commands: []string{
				"unikraft image build .",
			},
		},
		{
			Description: "Build and publish an image from a Kraftfile",
			Commands: []string{
				"unikraft image build . --output my-org/my-app:latest",
			},
		},
		{
			Description: "Build from a Dockerfile without a Kraftfile",
			Commands: []string{
				"unikraft image build ./Dockerfile --arch x86_64 --output my-org/my-app:latest",
			},
		},
		{
			Description: "Build with custom build arguments",
			Commands: []string{
				"unikraft image build ./app --build-arg VERSION=1.2.3 --build-arg COMMIT=abc123",
			},
		},
		{
			Description: "Build with secret and SSH access",
			Commands: []string{
				"unikraft image build . --secret id=npm,src=$HOME/.npmrc --ssh default=$SSH_AUTH_SOCK",
			},
		},
		{
			Description: "Build for arm64 and publish the image",
			Commands: []string{
				"unikraft image build . --arch arm64 --output my-org/my-app:latest",
			},
		},
		{
			Description: "Build and save to a local OCI archive",
			Commands: []string{
				"unikraft image build . --output ./dist/my-app.oci.tar",
			},
		},
	}
}

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

type BuildCmd struct {
	ImageBuildCmd `embed:""`
}

func (BuildCmd) Examples() []kingkong.Example {
	imageExamples := ImageBuildCmd{}.Examples()
	for i := range imageExamples {
		for j := range imageExamples[i].Commands {
			imageExamples[i].Commands[j] = strings.Replace(
				imageExamples[i].Commands[j],
				"unikraft image build",
				"unikraft build",
				1,
			)
		}
	}

	return imageExamples
}

func (c *BuildCmd) Run(ctx context.Context, cfg *config.Config, partition *resource.Partition) error {
	return c.ImageBuildCmd.Run(ctx, cfg, partition)
}
