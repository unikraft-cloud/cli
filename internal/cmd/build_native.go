// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/progress/progressui"
	"unikraft.com/x/build"
	"unikraft.com/x/build/buildflags"
	"unikraft.com/x/build/progress"
	"unikraft.com/x/buildkit"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/image-spec/schemes"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/resource"
)

// localBuildkit is the buildkitd that builds run on.
var localBuildkit = buildkit.Fallback{buildkit.Host{}, buildkit.Docker{}}

// newBuilder gives a builder that uses the current profile and the local
// BuildKit, and reports its progress to pw. It gets each of them only when the
// build needs it.
func newBuilder(pw progress.Writer) build.Builder {
	return build.Builder{
		Images: func(ctx context.Context) (*imagespec.Accessor, error) {
			return images.Accessor(ctx)
		},
		Buildkit: func(ctx context.Context) (*client.Client, func(), error) {
			d, err := localBuildkit.Acquire(ctx, buildkit.Key{})
			if errors.Is(err, buildkit.ErrUnavailable) {
				log.G(ctx).Debug().Err(err).Msg("no buildkit")
				return nil, nil, errors.New("buildkit not found; please start a docker or buildkit daemon, or set BUILDKIT_HOST")
			}
			if err != nil {
				return nil, nil, err
			}
			log.G(ctx).Info().Str("addr", d.Addr).Msg("found buildkit")
			return d.Client, func() { _ = d.Release() }, nil
		},
		Session: func(ctx context.Context) ([]session.Attachable, error) {
			auth, err := authAttachable(ctx)
			if err != nil {
				return nil, err
			}
			return []session.Attachable{auth}, nil
		},
		Progress: pw,
	}
}

// authAttachable answers registry logins from the docker config and the current profile.
func authAttachable(ctx context.Context) (session.Attachable, error) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	dockerConfig := dockerconfig.LoadDefaultConfigFile(os.Stderr)
	return authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
		AuthConfigProvider: images.LoadBuildkitAuthConfig(dockerConfig, profile),
	}), nil
}

// buildWithProgress runs fn on a builder that draws its progress to stderr,
// and returns once the display has finished drawing.
func buildWithProgress(ctx context.Context, fn func(build.Builder) error) error {
	disp, err := progress.NewDisplay(ctx, os.Stderr, progressui.AutoMode)
	if err != nil {
		return err
	}
	err = fn(newBuilder(disp))
	if cerr := disp.Close(); err == nil {
		err = cerr
	}
	return err
}

func (c *ImageBuildCmd) Run(ctx context.Context, cfg *config.Config, partition *resource.Partition) error {
	if c.Remote != "" {
		return c.runRemote(ctx, cfg, partition)
	}

	buildOpts, err := build.LoadBuildOpts(ctx, c.Input)
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

	var imgs []*imagespec.Image
	err = buildWithProgress(ctx, func(b build.Builder) (err error) {
		imgs, err = b.Build(ctx, buildOpts)
		return err
	})
	if errors.Is(err, build.ErrNoRuntimeTarget) {
		return fmt.Errorf("use --arch to select another target: %w", err)
	} else if err != nil {
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
	output, err := imagespec.GuessLocation(c.Output)
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
	_, err = access.Save(ctx, output, imgs...)
	if err != nil {
		return err
	}
	log.G(ctx).Info().
		Str("output", output.String()).
		Msg("saved image")

	if partition != nil && output.Scheme == schemes.OCI {
		if err := addImageToPartition(ctx, partition, output.Path); err != nil {
			return fmt.Errorf("adding built image to partition: %w", err)
		}
	}

	return nil
}

func (c *BuildCmd) Run(ctx context.Context, cfg *config.Config, partition *resource.Partition) error {
	return c.ImageBuildCmd.Run(ctx, cfg, partition)
}
