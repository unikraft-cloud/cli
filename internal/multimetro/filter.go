// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"context"

	"github.com/containerd/containerd/v2/pkg/filters"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
)

func filterMetrosFromContext(ctx context.Context, metros []config.Metro) []config.Metro {
	spec := resource.FilterFromContext(ctx)
	return filterMetros(metros, spec)
}

func filterMetros(metros []config.Metro, spec filters.Filter) []config.Metro {
	names := filterMetroNames(metros, spec)
	if len(names) == 0 {
		// filter did not seem to apply to any metros
		return metros
	}

	result := make([]config.Metro, 0, len(names))
	for _, metro := range metros { // preserve order
		if ok := names[metro.Name.String()]; ok {
			result = append(result, metro)
		}
	}
	return result
}

func filterMetroNames(metros []config.Metro, spec filters.Filter) map[string]bool {
	result := make(map[string]bool)
	switch spec := spec.(type) {
	case filters.All:
		for _, sub := range spec {
			filtered := filterMetroNames(metros, sub)
			for k, v := range filtered {
				if _, exists := result[k]; !exists {
					result[k] = true
				}
				result[k] = result[k] && v
			}
		}
		return result
	case filters.Any:
		for _, sub := range spec {
			filtered := filterMetroNames(metros, sub)
			if len(filtered) == 0 {
				// filter did not seem to apply to any metros, so include all of them
				for _, metro := range metros {
					result[metro.Name.String()] = true
				}
				break
			}
			for k, v := range filtered {
				result[k] = result[k] || v
			}
		}
		return result
	default:
		for _, metro := range metros {
			var found bool
			matched := spec.Match(filters.AdapterFunc(func(fieldpath []string) (string, bool) {
				if len(fieldpath) != 1 {
					return "", false
				}
				if fieldpath[0] != "metro" {
					return "", false
				}
				found = true
				return metro.Name.String(), true
			}))
			if found {
				result[metro.Name.String()] = matched
			}
		}
		return result
	}
}
