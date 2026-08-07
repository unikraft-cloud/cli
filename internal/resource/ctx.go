// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package resource

import (
	"context"

	"unikraft.com/x/filters"
)

type contextKeyFilters struct{}

func WithFilter(ctx context.Context, spec filters.Filter) context.Context {
	return context.WithValue(ctx, contextKeyFilters{}, spec)
}

func FilterFromContext(ctx context.Context) filters.Filter {
	spec, ok := ctx.Value(contextKeyFilters{}).(filters.Filter)
	if ok {
		return spec
	}
	return filters.Always
}

type contextKeySandbox struct{}

// WithSandbox stores a Sandbox in the context so that operations performed
// during a Rollout can use sandbox-aware create/delete wrappers.
func WithSandbox(ctx context.Context, s *Sandbox) context.Context {
	return context.WithValue(ctx, contextKeySandbox{}, s)
}

// SandboxFromContext retrieves the Sandbox stored by WithSandbox. Returns nil
// if no sandbox was stored (non-sandboxed execution).
func SandboxFromContext(ctx context.Context) *Sandbox {
	s, _ := ctx.Value(contextKeySandbox{}).(*Sandbox)
	return s
}
