// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package jsonschema

import (
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

func SingleTypes(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	if len(schema.Types) > 0 {
		types := slices.DeleteFunc(slices.Clone(schema.Types), func(t string) bool { return t == "null" })
		if len(types) == 1 {
			schema.Type, schema.Types = types[0], nil
		}
	}
	SingleTypes(schema.Items)
	for _, prop := range schema.Properties {
		SingleTypes(prop)
	}
}

func WithoutNulls(v any) any {
	if m, ok := v.(map[string]any); ok {
		for k, e := range m {
			if e == nil {
				delete(m, k)
			} else {
				WithoutNulls(e)
			}
		}
	}
	return v
}
