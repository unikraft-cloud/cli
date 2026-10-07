// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"fmt"
	"os"

	"github.com/containerd/platforms"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/image-spec/schemes"

	"unikraft.com/cli/internal/images"
)

// LoadKernel wraps an already-built kernel binary as an image. A kernel binary
// carries no platform metadata of its own, so exactly one platform must be
// requested, and it is taken on trust.
func LoadKernel(_ context.Context, opts BuildOpts) ([]*imagespec.Image, error) {
	if len(opts.Platform) != 1 {
		return nil, fmt.Errorf("a kernel must be built for exactly one target, got %d", len(opts.Platform))
	}

	f, err := os.Open(opts.Kernel)
	if err != nil {
		return nil, fmt.Errorf("opening kernel: %w", err)
	}

	return []*imagespec.Image{
		imagespec.NewImage(
			imagespec.WithPlatform(opts.Platform[0]),
			imagespec.WithKernel(imagespec.NewOSFile(f)),
		),
	}, nil
}

func BuildKernel(ctx context.Context, opts BuildOpts) ([]*imagespec.Image, error) {
	access, err := images.Accessor(ctx)
	if err != nil {
		return nil, err
	}

	runtime, err := imagespec.ParseLocationDefault(opts.Runtime)
	if err != nil {
		return nil, fmt.Errorf("parsing runtime reference: %w", err)
	}
	if runtime.Scheme != schemes.OCI {
		return nil, fmt.Errorf("unsupported runtime reference scheme: %s", runtime.Scheme)
	}

	var platform platforms.MatchComparer
	if opts.Platform == nil {
		platform = platforms.All
	} else {
		platform = platforms.Any(opts.Platform...)
	}

	imgs, err := access.LoadAll(ctx, runtime, platform)
	if err != nil {
		return nil, fmt.Errorf("loading runtime image: %w", err)
	}

	imgPlatforms := map[string]struct{}{}
	for _, img := range imgs {
		imgPlatforms[platforms.Format(platforms.Normalize(img.Image.Platform))] = struct{}{}
	}
	var missingPlatforms []string
	for _, p := range opts.Platform {
		if _, ok := imgPlatforms[platforms.Format(platforms.Normalize(p))]; !ok {
			missingPlatforms = append(missingPlatforms, platforms.Format(p))
		}
	}
	if len(missingPlatforms) > 0 {
		return nil, fmt.Errorf("runtime image does not contain required platforms: %v", missingPlatforms)
	}

	return imgs, nil
}
