// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import (
	"context"
	"fmt"

	"github.com/containerd/containerd/v2/core/remotes/docker"
	ociref "github.com/distribution/reference"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/image-spec/imageref"

	"unikraft.com/cli/internal/config"
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
		imagespec.WithReferenceParser(ParseName),
	), nil
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

// officialPrefix is the namespace Unikraft's registry serves its own images
// from.
const officialPrefix = "official/"

// Policy is what an image identifier is allowed to leave implicit: the registry
// it lives in and the namespace within it.
type Policy struct {
	// Domain is the registry an identifier without one names.
	Domain string

	// Prefix is the namespace a single-segment repository on Domain names.
	Prefix string
}

// PolicyFor returns the policy for identifiers exchanged with metro, or the
// default registry's policy when there is no metro in scope.
func PolicyFor(metro *config.Metro) Policy {
	domain := DefaultRegistry
	if metro != nil {
		domain = metro.Index().Host
	}
	return Policy{Domain: domain, Prefix: officialPrefix}
}

// Parse parses an image identifier supplied by the user or returned by the API.
func (p Policy) Parse(key string) (imageref.Reference, error) {
	ref, err := imageref.Parse(key,
		imageref.WithDefaultDomain(p.Domain),
		imageref.WithDefaultPrefix(p.Prefix),
	)
	if err != nil {
		return imageref.Reference{}, err
	}
	return p.canonical(ref)
}

// canonical returns ref on the canonical spelling of its registry, so that the
// same image reached through a metro's index and through the default registry
// deduplicates to one entry rather than being listed twice.
func (p Policy) canonical(ref imageref.Reference) (imageref.Reference, error) {
	if p.Domain == DefaultRegistry || ref.Scheme() != imageref.SchemeOCI || ref.Domain() != p.Domain {
		return ref, nil
	}
	return ref.WithDomain(DefaultRegistry)
}

// Format renders ref the way the CLI displays an image: an HTTP-served layout
// by the URI it is fetched from, and a registry image in its familiar short
// form.
func (p Policy) Format(ref imageref.Reference, short bool) string {
	// Parse canonicalizes p.Domain onto DefaultRegistry, so eliding
	// DefaultRegistry here - not p.Domain.
	return ref.WithoutDefaultTag().Format(imageref.FormatOpts{
		OmitDigest:    short,
		DefaultDomain: DefaultRegistry,
		DefaultPrefix: p.Prefix,
	})
}

// ParseRef parses an image identifier without metro context.
func ParseRef(key string) (imageref.Reference, error) {
	return PolicyFor(nil).Parse(key)
}

// ParseRefMetro parses an image identifier exchanged with metro, applying its
// index as the default registry domain.
func ParseRefMetro(metro *config.Metro, key string) (imageref.Reference, error) {
	return PolicyFor(metro).Parse(key)
}

// ParseName parses an image identifier and returns just the OCI name it
// decomposes to.
func ParseName(key string) (ociref.Named, error) {
	ref, err := ParseRef(key)
	if err != nil {
		return nil, err
	}
	named := ref.Named()
	if named == nil {
		return nil, fmt.Errorf("image %q is served over %s, so it has no registry name", key, ref.Scheme())
	}
	return named, nil
}

// Format renders ref for display, against the default registry's policy.
func Format(ref imageref.Reference) string {
	return PolicyFor(nil).Format(ref, false)
}

// FormatShort renders ref for concise display, eliding the digest.
func FormatShort(ref imageref.Reference) string {
	return PolicyFor(nil).Format(ref, true)
}
