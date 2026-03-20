// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	imagespec "unikraft.com/x/image-spec"

	ximagespec "unikraft.com/cli/internal/x/imagespec"
	"unikraft.com/x/kraftfile"
)

type BuildOpts struct {
	Rootfs RootfsOpts

	Runtime string

	Platform []ocispec.Platform

	Cmd    []string
	Env    kraftfile.Map
	Labels map[string]string
}

type RootfsOpts struct {
	Path string

	// Output params
	Format     kraftfile.FsType
	Compress   bool
	KeepOwners bool

	// Buildkit params
	// Dockerfile string
	BuildArg []string
	Target   string
	Secrets  []*Secret
	SSH      []*SSH

	NoCache bool
}

type RootfsType string

const (
	RootfsTypeDockerfile RootfsType = "dockerfile"
)

// Build a unikraft image based on the provided build options.
func Build(ctx context.Context, opts BuildOpts, c *client.Client) ([]*imagespec.Image, error) {
	kernels, err := BuildKernel(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, kernel := range kernels {
			kernel.Close()
		}
	}()
	opts.Platform = make([]ocispec.Platform, 0, len(kernels))
	for _, kernel := range kernels {
		opts.Platform = append(opts.Platform, kernel.Image.Platform)
	}

	roots, err := BuildRootfs(ctx, c, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, root := range roots {
			root.Close()
		}
	}()

	meta := imagespec.ImageMetadata{
		Created: new(time.Now()),
	}

	if len(kernels) != len(roots) {
		panic(fmt.Sprintf("internal error: number of kernels (%d) does not match number of root filesystems (%d)", len(kernels), len(roots)))
	}

	images := make([]*imagespec.Image, 0, len(kernels))
	for i, kernel := range kernels {
		root := roots[i]

		if platforms.Format(kernel.Image.Platform) != platforms.Format(root.Image.Platform) {
			panic(fmt.Sprintf("internal error: kernel platform (%s) does not match rootfs platform (%s)",
				platforms.Format(kernel.Image.Platform),
				platforms.Format(root.Image.Platform)),
			)
		}

		images = append(images, imagespec.NewImage(
			imagespec.WithKernel(ximagespec.WrapCached(ctx, kernel.Kernel)),
			imagespec.WithInitrd(ximagespec.WrapCached(ctx, root.Initrd)),
			imagespec.WithImageConfig(root.Image.Config),
			imagespec.WithImageMetadata(meta),
			imagespec.WithPlatform(root.Image.Platform),
		))

		// ensure we don't cleanup the kernel and initrd files we use above
		kernel.Kernel = nil
		root.Initrd = nil
	}

	return images, nil
}
