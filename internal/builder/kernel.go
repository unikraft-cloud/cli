// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"fmt"

	"github.com/containerd/platforms"
	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/cli/internal/images"
)

func BuildKernel(ctx context.Context, opts BuildOpts) ([]*imagespec.Image, error) {
	access, err := images.Accessor(ctx)
	if err != nil {
		return nil, err
	}

	runtime, err := imagespec.ParseURIDefault(opts.Runtime)
	if err != nil {
		return nil, fmt.Errorf("parsing runtime reference: %w", err)
	}
	if runtime.Scheme != imagespec.URISchemeOCI {
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
