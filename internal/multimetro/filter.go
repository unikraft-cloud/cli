// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"context"

	"unikraft.com/x/filters"

	"unikraft.com/cli/internal/model"
)

func filterMetrosFromCtx(ctx context.Context, names []string) []string {
	return filterMetros(names, model.FilterFromContext(ctx))
}

func filterMetros(names []string, spec filters.Filter) []string {
	if spec == nil {
		return names
	}
	return filters.Restrict(spec, "metro", names)
}
