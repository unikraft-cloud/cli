// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package value

import (
	"encoding"
	"fmt"
	"iter"
	"reflect"

	"github.com/ettle/strcase"
)

type namedField struct {
	Name      string
	Value     reflect.Value
	Anonymous bool
	Type      reflect.Type
}

// namedFields yields the fields of struct v keyed by their "name" tag, which
// is a separate namespace from the "field" tag so that a field excluded from
// the field system stays settable. Parse and Render share this so a key they
// disagree on cannot exist.
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
				Type:      field.Type,
			}) {
				return
			}
		}
	}
}

// structValue returns f as a struct for a dotted path to descend into. An
// embedded field is skipped, having no key of its own to sit under.
//
// alloc fills in nil pointers on the way, for parsing; rendering leaves them
// alone and so finds nothing to descend into.
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

// implements reports whether f, or a pointer to it, satisfies any of ifaces.
// Converting to or from a string as a whole makes a field opaque: its own
// fields are not separately addressable.
func (f namedField) implements(ifaces ...reflect.Type) bool {
	for _, iface := range ifaces {
		if f.Type.Implements(iface) || reflect.PointerTo(f.Type).Implements(iface) {
			return true
		}
	}
	return false
}

var (
	textUnmarshaler = reflect.TypeFor[encoding.TextUnmarshaler]()
	textMarshaler   = reflect.TypeFor[encoding.TextMarshaler]()
	stringer        = reflect.TypeFor[fmt.Stringer]()
	renderer        = reflect.TypeFor[Renderer]()
)

// walkFields yields every leaf of struct v as the dotted path addressing it.
// Flattening is what lets a rendered struct parse back: rendered inline, a
// nested struct's commas reparse as fields of the outer struct.
func walkFields(v reflect.Value, prefix string) iter.Seq2[string, reflect.Value] {
	return func(yield func(string, reflect.Value) bool) {
		for field := range namedFields(v) {
			path := prefix + field.Name
			if sub, ok := field.structValue(false); ok &&
				!field.implements(renderer, stringer, textMarshaler) {
				for path, leaf := range walkFields(sub, path+".") {
					if !yield(path, leaf) {
						return
					}
				}
				continue
			}
			if !yield(path, field.Value) {
				return
			}
		}
	}
}
