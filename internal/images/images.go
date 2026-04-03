// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import (
	"context"
	"fmt"

	"github.com/distribution/reference"
	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/cli/internal/config"
	xreference "unikraft.com/cli/internal/x/reference"
)

const DefaultRegistry = "unikraft.io"

var defaultRegistries = []string{
	"unikraft.io",
	"index.unikraft.io",
}

// AccessorOpt is a functional option for configuring an Accessor.
type AccessorOpt func(*accessorOpts)

type accessorOpts struct {
	skipTLSVerify bool
}

// WithSkipTLSVerify configures the accessor to skip TLS certificate
// verification when communicating with registries.
func WithSkipTLSVerify(skip bool) AccessorOpt {
	return func(o *accessorOpts) {
		o.skipTLSVerify = skip
	}
}

func Accessor(ctx context.Context, opts ...AccessorOpt) (*imagespec.Accessor, error) {
	var o accessorOpts
	for _, opt := range opts {
		opt(&o)
	}

	cfg := config.FromContextOrDefault(ctx)
	profile, err := cfg.CurrentProfile()
	if err != nil {
		return nil, err
	}

	return imagespec.NewAccessor(
		imagespec.WithResolver(Resolver(profile, o.skipTLSVerify)),
		imagespec.WithReferenceParser(ParseNormalizedNamed),
	), nil
}

func ParseNormalizedNamed(key string) (reference.Named, error) {
	return ParseNormalizedNamedMetro(nil, key)
}

func ParseNormalizedNamedMetro(metro *config.Metro, key string) (reference.Named, error) {
	if uri, err := imagespec.ParseURI(key); err == nil {
		if uri.Scheme != imagespec.URISchemeOCI {
			return nil, fmt.Errorf("%w: invalid scheme %q", reference.ErrReferenceInvalidFormat, uri.Scheme)
		}
		key = uri.Path
	}

	index := DefaultRegistry
	if metro != nil {
		index = metro.Index().Host
	}
	return xreference.ParseNormalizedNamed(
		key,
		xreference.WithDefaultDomain(index),
		xreference.WithDefaultPrefix("official/"),
	)
}

func FamiliarString(ref reference.Reference) string {
	return xreference.FamiliarString(
		ref,
		xreference.WithDefaultDomain(DefaultRegistry),
		xreference.WithDefaultPrefix("official/"),
	)
}
