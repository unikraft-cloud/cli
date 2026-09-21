// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import (
	"context"
	"fmt"

	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/core/remotes/docker"
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

type insecureContextKey struct{}

// WithInsecureContext returns a context that carries insecure registry options,
// which are picked up by Accessor.
func WithInsecureContext(ctx context.Context, opts ...AccessorOpt) context.Context {
	return context.WithValue(ctx, insecureContextKey{}, opts)
}

func Accessor(ctx context.Context, opts ...AccessorOpt) (*imagespec.Accessor, error) {
	options, err := resolverOptionsFor(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return imagespec.NewAccessor(
		imagespec.WithResolver(docker.NewResolver(options)),
		imagespec.WithRegistryHosts(options.Hosts),
		imagespec.WithRegistryHeaders(options.Headers),
		imagespec.WithReferenceParser(ParseNormalizedNamed),
	), nil
}

// Resolver gives the registry resolver that Accessor uses, for callers that
// fetch or push content themselves.
func Resolver(ctx context.Context, opts ...AccessorOpt) (remotes.Resolver, error) {
	options, err := resolverOptionsFor(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return docker.NewResolver(options), nil
}

// resolverOptionsFor builds the registry options from the profile in ctx. With
// no opts, the insecure options that WithInsecureContext carries are used.
func resolverOptionsFor(ctx context.Context, opts ...AccessorOpt) (docker.ResolverOptions, error) {
	if len(opts) == 0 {
		if ctxOpts, ok := ctx.Value(insecureContextKey{}).([]AccessorOpt); ok {
			opts = ctxOpts
		}
	}

	var o accessorOpts
	for _, opt := range opts {
		opt(&o)
	}

	profile, err := config.FromContextOrDefault(ctx).CurrentProfile()
	if err != nil {
		return docker.ResolverOptions{}, err
	}

	return resolverOptions(profile, o.insecureRegistries, o.allInsecure), nil
}

// AccessorOpt is a functional option for configuring an Accessor.
type AccessorOpt func(*accessorOpts)

type accessorOpts struct {
	insecureRegistries []string
	allInsecure        bool
}

func WithInsecureRegistry(hosts ...string) AccessorOpt {
	return func(o *accessorOpts) {
		o.insecureRegistries = hosts
	}
}

func WithInsecureRegistries() AccessorOpt {
	return func(o *accessorOpts) {
		o.allInsecure = true
	}
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
