// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package jason

import (
	"encoding"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// literal is a '=' value, which is a string unless its target type says
// otherwise.
type literal string

// coerce converts every literal in node into the JSON type its destination in
// t expects, leaving it a string when t doesn't know better.
func coerce(node any, t reflect.Type) any {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch n := node.(type) {
	case literal:
		return coerceLiteral(string(n), t)
	case map[string]any:
		for k, v := range n {
			n[k] = coerce(v, keyType(t, k))
		}
	case []any:
		var elem reflect.Type
		if t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
			elem = t.Elem()
		}
		for i, v := range n {
			n[i] = coerce(v, elem)
		}
	}
	return node
}

func coerceLiteral(s string, t reflect.Type) any {
	if t == nil {
		return s
	}
	ptr := reflect.PointerTo(t)
	if ptr.Implements(reflect.TypeFor[json.Unmarshaler]()) || ptr.Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		return s
	}
	switch t.Kind() {
	case reflect.Bool:
		if b, err := strconv.ParseBool(s); err == nil {
			return b
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		if isJSONNumber(s) {
			return json.Number(s)
		}
	}
	return s
}

func isJSONNumber(s string) bool {
	return s != "" && (s[0] == '-' || (s[0] >= '0' && s[0] <= '9')) && json.Valid([]byte(s))
}

// keyType returns the type that key decodes into within t, following
// encoding/json's field naming, or nil when unknown.
func keyType(t reflect.Type, key string) reflect.Type {
	if t == nil {
		return nil
	}
	switch t.Kind() {
	case reflect.Map:
		return t.Elem()
	case reflect.Struct:
	default:
		return nil
	}

	var folded reflect.Type
	for _, f := range reflect.VisibleFields(t) {
		if f.Anonymous && f.Tag.Get("json") == "" && f.Type.Kind() == reflect.Struct {
			continue
		}
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		typ := f.Type
		if slices.Contains(strings.Split(opts, ","), "string") {
			// The ",string" option wants the number quoted.
			typ = nil
		}
		if name == key {
			return typ
		}
		if folded == nil && strings.EqualFold(name, key) {
			folded = typ
		}
	}
	return folded
}
