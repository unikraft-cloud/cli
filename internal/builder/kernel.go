// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"fmt"

	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/image-spec/imageref"

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
	if runtime.Scheme != imageref.SchemeOCI {
		return nil, fmt.Errorf("unsupported runtime reference scheme: %s", runtime.Scheme)
	}

	var platform platforms.MatchComparer
	if opts.Platform == nil {
		platform = platforms.All
	} else {
		platform = ignoringOSFeatures(platforms.Any(opts.Platform...))
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

// ignoringOSFeatures wraps a MatchComparer and strips OSFeatures from the
// candidate platform before delegating to the inner matcher. In
// containerd/platforms v1, NewMatcher requires the matcher's OSFeatures to be
// a superset of the candidate's, so a spec without OSFeatures would fail to
// match any manifest entry that carries them (e.g. EROFS feature flags). By
// stripping OSFeatures from the candidate at match time we get OS/arch/variant
// matching semantics regardless of what features the manifest entry advertises.
type ignoringOSFeaturesMatcher struct {
	platforms.MatchComparer
}

func ignoringOSFeatures(m platforms.MatchComparer) platforms.MatchComparer {
	return ignoringOSFeaturesMatcher{m}
}

func (m ignoringOSFeaturesMatcher) Match(p ocispec.Platform) bool {
	p.OSFeatures = nil
	return m.MatchComparer.Match(p)
}

func (m ignoringOSFeaturesMatcher) Less(p1, p2 ocispec.Platform) bool {
	p1.OSFeatures = nil
	p2.OSFeatures = nil
	return m.MatchComparer.Less(p1, p2)
}
