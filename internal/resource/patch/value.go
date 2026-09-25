// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"unikraft.com/cli/internal/resource"
)

// CreateValue reads the value a create sets at path.
func CreateValue[T any](fields []resource.Field, path string) (T, bool) {
	for _, field := range resource.GetFieldByPathString(fields, path) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		if v, ok := field.Create.Set.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// SetCreateValue sets what a create applies at path, replacing any patch the
// flags produced.
func SetCreateValue(fields []resource.Field, path string, set any) []resource.Field {
	found := false
	for _, field := range resource.GetFieldByPathString(fields, path) {
		if field.Create == nil {
			continue
		}
		field.Create.Set = set
		found = true
	}
	if found {
		return fields
	}
	return append(fields, resource.Field{Name: path, Create: &resource.Patch{Set: set}})
}
