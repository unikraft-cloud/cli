// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package images

import (
	"context"

	"github.com/containerd/containerd/v2/core/remotes/docker"
	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/cli/internal/config"
)

var defaultRegistries = []string{
	"unikraft.io",
	"index.unikraft.io",
}

// Accessor builds a registry accessor for the current profile.
func Accessor(ctx context.Context, opts ...AccessorOpt) (*imagespec.Accessor, error) {
	if len(opts) == 0 {
		if ctxOpts, ok := ctx.Value(insecureContextKey{}).([]AccessorOpt); ok {
			opts = ctxOpts
		}
	}

	var o accessorOpts
	for _, opt := range opts {
		opt(&o)
	}

	cfg := config.FromContextOrDefault(ctx)
	profile, err := cfg.CurrentProfile()
	if err != nil {
		return nil, err
	}

	options := resolverOptions(profile, o.insecureRegistries, o.allInsecure)
	resolver := docker.NewResolver(options)
	return imagespec.NewAccessor(
		imagespec.WithResolver(resolver),
		imagespec.WithRegistryHosts(options.Hosts),
		imagespec.WithRegistryHeaders(options.Headers),
		imagespec.WithReferenceParser(ParseNormalizedNamed),
	), nil
}
