// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"

	"golang.org/x/mod/semver"
	"unikraft.com/cli/internal/builder"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"
)

type BuildCmd struct {
	Input  string `arg:"" default:"." help:"Path to the input directory."`
	Output string `short:"o" help:"Output destination"`

	// similar to docker compose build
	BuildArg []string `help:"Set build-time variables."`
	NoCache  bool     `help:"Do not use cache when building the image."`
	Secret   []string `help:"Secret to expose to the build (format: \"id=mysecret[,src=/local/secret]\")."`
	SSH      []string `help:"SSH agent socket or keys to expose to the build (format: \"default|<id>[=<socket>|<key>[,<key>]]\")."`
}

func (BuildCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Build the project in the current directory",
			Commands: []string{
				"unikraft build .",
			},
		},
		{
			Description: "Build and publish an image to a registry",
			Commands: []string{
				"unikraft build . --output my-org/my-app:latest",
			},
		},
		{
			Description: "Build with custom build arguments",
			Commands: []string{
				"unikraft build ./app --build-arg VERSION=1.2.3 --build-arg COMMIT=abc123",
			},
		},
		{
			Description: "Build without using cache",
			Commands: []string{
				"unikraft build . --no-cache",
			},
		},
		{
			Description: "Build with secret and SSH access",
			Commands: []string{
				"unikraft build . --secret id=npm,src=$HOME/.npmrc --ssh default=$SSH_AUTH_SOCK",
			},
		},
		{
			Description: "Build and save to a local OCI archive",
			Commands: []string{
				"unikraft build . --output ./dist/my-app.oci.tar",
			},
		},
	}
}

func (c *BuildCmd) Run(ctx context.Context, cfg *config.Config) error {
	kf, err := kraftfile.ParseDirectory(c.Input, kraftfile.WithSkippedVersionCheck())
	if err != nil {
		return err
	}
	if semver.Compare(kf.Spec, kraftfile.SpecVersionMin) < 0 {
		log.G(ctx).Warn().
			Str("spec", kf.Spec).
			Str("min", kraftfile.SpecVersionMin).
			Msg("Kraftfile spec version is older than minimum; parsing is best-effort")
	} else if semver.Compare(kf.Spec, kraftfile.SpecVersionMax) > 0 {
		log.G(ctx).Warn().
			Str("spec", kf.Spec).
			Str("max", kraftfile.SpecVersionMax).
			Msg("Kraftfile spec version is newer than maximum; parsing is best-effort")
	}

	buildOpts, err := builder.KraftfileToBuildOpts(c.Input, kf)
	if err != nil {
		return err
	}

	if len(c.BuildArg) > 0 {
		buildOpts.Rootfs.BuildArg = append(buildOpts.Rootfs.BuildArg, c.BuildArg...)
	}
	if c.NoCache {
		buildOpts.Rootfs.NoCache = true
	}
	if len(c.Secret) > 0 {
		secrets, err := builder.ParseSecretSpecs(c.Secret)
		if err != nil {
			return err
		}
		buildOpts.Rootfs.Secrets = secrets
	}
	if len(c.SSH) > 0 {
		ssh, err := builder.ParseSSHSpecs(c.SSH)
		if err != nil {
			return err
		}
		buildOpts.Rootfs.SSH = ssh
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

	access, err := images.Accessor(ctx)
	if err != nil {
		return err
	}
	err = access.Save(ctx, output, imgs...)
	if err != nil {
		return err
	}

	return nil
}
