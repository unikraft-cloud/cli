// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"errors"
	"fmt"

	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/image-spec/schemes"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/images"
	wplatforms "unikraft.com/cli/internal/w/platforms"
)

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

	var matcher platforms.MatchComparer
	switch {
	case len(opts.Platform) > 0:
		matcher = platforms.Any(opts.Platform...)
	case opts.PlatformMatcher != nil:
		matcher = opts.PlatformMatcher
	default:
		// Runtimes used to ship x86_64 only, so Kraftfiles without targets
		// keep building just that rather than every architecture added since.
		log.G(ctx).Info().
			Str("arch", imagespec.ArchitectureIntel).
			Msg("no target specified, using default architecture")
		matcher = wplatforms.OnlyArch(imagespec.ArchitectureIntel)
	}

	imgs, err := access.LoadAll(ctx, runtime, matcher)
	if err != nil {
		if len(opts.Platform) == 0 && errors.Is(err, errdefs.ErrNotFound) {
			return nil, fmt.Errorf("runtime has no %s target, use --arch to select another: %w", matcher, err)
		}
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
