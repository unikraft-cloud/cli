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
