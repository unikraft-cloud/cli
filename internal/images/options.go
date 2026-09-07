// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import "context"

type insecureContextKey struct{}

// WithInsecureContext returns a context that carries insecure registry options,
// which are picked up by Accessor.
func WithInsecureContext(ctx context.Context, opts ...AccessorOpt) context.Context {
	return context.WithValue(ctx, insecureContextKey{}, opts)
}

// AccessorOpt is a functional option for configuring an Accessor.
type AccessorOpt func(*accessorOpts)

type accessorOpts struct {
	insecureRegistries []string
	allInsecure        bool
}

// WithInsecureRegistry allows insecure connections to the named hosts.
func WithInsecureRegistry(hosts ...string) AccessorOpt {
	return func(o *accessorOpts) {
		o.insecureRegistries = hosts
	}
}

// WithInsecureRegistries allows insecure connections to every host.
func WithInsecureRegistries() AccessorOpt {
	return func(o *accessorOpts) {
		o.allInsecure = true
	}
}
