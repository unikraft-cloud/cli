// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package value

import (
	"iter"
	"reflect"

	"github.com/ettle/strcase"
)

type namedField struct {
	Name      string
	Value     reflect.Value
	Anonymous bool
}

// namedFields gives the exported fields of struct v by their "name" tag.
// Parse and Render both use it, so they use the same keys.
func namedFields(v reflect.Value) iter.Seq[namedField] {
	return func(yield func(namedField) bool) {
		t := v.Type()
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name := field.Tag.Get("name")
			if name == "-" {
				continue
			}
			if name == "" {
				name = strcase.ToKebab(field.Name)
			}
			if !yield(namedField{
				Name:      name,
				Value:     v.Field(i),
				Anonymous: field.Anonymous,
			}) {
				return
			}
		}
	}
}

// structValue returns f as a struct that a dotted key can go into.
// It returns false for an embedded field, because it has no key.
func (f namedField) structValue(alloc bool) (reflect.Value, bool) {
	if f.Anonymous {
		return reflect.Value{}, false
	}
	v := f.Value
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			if !alloc {
				return reflect.Value{}, false
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return v, true
}

// implements reports if f, or a pointer to f, implements one of ifaces.
// A field that converts to or from text is one value with no subkeys.
func (f namedField) implements(ifaces ...reflect.Type) bool {
	t := f.Value.Type()
	for _, iface := range ifaces {
		if t.Implements(iface) || reflect.PointerTo(t).Implements(iface) {
			return true
		}
	}
	return false
}
