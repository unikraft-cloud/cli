// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package jason (JavaScript Advanced Object Notation) provides helpers for decoding standard JSON and HTTPie-style
// nested-item input into a typed value.
//
// Nested-item input expresses a path to each value using bracket notation.
// Use '=' for literal strings and ':=' for raw JSON values.
//
// See https://httpie.io/docs/cli/nested-json for the input format.
//
// I first named it nested, but it's not only nested, it's nested-JSON + JSON together.
// Couldn't find a better name, jason was closest to my head because of a meme stuck in my head from youtube I guess. Prob Primeagen can't remember.
//
// why name it JASON? https://github.com/unikraft-cloud/cli/pull/389#discussion_r3524002021
package jason

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Jason holds a value of type T and can populate it from either standard JSON
// or nested-item input.
type Jason[T any] struct {
	Value T
}

// Unmarshal decodes JSON first and falls back to nested-item input when the
// payload is not valid JSON.
func Unmarshal[T any](data []byte, v *Jason[T]) error {
	if err := json.Unmarshal(data, &v.Value); err == nil {
		return nil
	}
	input := strings.TrimSpace(string(data))
	if input == "" {
		// Best-effort: T may not be object-shaped (e.g. a slice or scalar),
		// in which case this fails without touching v.Value, leaving it at
		// its zero value.
		_ = json.Unmarshal([]byte("{}"), &v.Value)
		return nil
	}
	items, err := parseItems(input)
	if err != nil {
		return err
	}
	return UnmarshalItems(items, &v.Value)
}

// UnmarshalItems decodes parsed nested items into v, which must be a pointer.
//
// A literal ('=') value is decoded as a boolean or number when that is what
// its destination in v holds, so ':=' is only needed where v can't tell.
func UnmarshalItems(items []Item, v any) error {
	root, err := buildTree(items)
	if err != nil {
		return err
	}
	data, err := json.Marshal(coerce(root, reflect.TypeOf(v)))
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return json.Unmarshal(data, v)
}

// Marshal encodes the wrapped value as JSON.
func Marshal[T any](v Jason[T]) ([]byte, error) {
	return json.Marshal(v.Value)
}
