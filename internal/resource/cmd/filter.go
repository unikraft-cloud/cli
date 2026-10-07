// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/charmbracelet/x/ansi"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/value"
	wtime "unikraft.com/cli/internal/w/time"
	"unikraft.com/x/filters"
)

type fieldAdaptor struct {
	children []resource.Field
	field    *resource.Field
	entries  []string
}

func newFieldAdaptor(fields []resource.Field) filters.Adaptor {
	return &fieldAdaptor{children: fields}
}

func (a *fieldAdaptor) Select(key []string) (filters.Adaptor, bool) {
	if len(key) == 0 && a.field != nil {
		return a, true
	}

	matched := resource.GetFieldByPath(a.children, key)
	if len(matched) == 0 {
		if len(key) >= 1 {
			parentKey := key[:len(key)-1]
			var parent *resource.Field
			if len(parentKey) == 0 {
				parent = a.field
			} else if pm := resource.GetFieldByPath(a.children, parentKey); len(pm) == 1 {
				parent = &pm[0]
			}
			if parent != nil {
				if slice, sok := getSliceValue(parent.Value); sok {
					idx, err := strconv.Atoi(key[len(key)-1])
					if err == nil && idx >= 0 && idx < len(slice) {
						f := resource.Field{Name: key[len(key)-1], Value: slice[idx]}
						return &fieldAdaptor{field: &f}, true
					}
				}
			}
		}
		return nil, false
	}

	if len(matched) == 1 {
		f := matched[0]
		if len(f.Subfields) > 0 {
			names := make([]string, len(f.Subfields))
			for i, sub := range f.Subfields {
				names[i] = sub.Name
			}
			return &fieldAdaptor{children: f.Subfields, field: &f, entries: names}, true
		}
		if slice, sok := getSliceValue(f.Value); sok {
			names := make([]string, len(slice))
			for i := range slice {
				names[i] = strconv.Itoa(i)
			}
			return &fieldAdaptor{field: &f, entries: names}, true
		}
		return &fieldAdaptor{field: &f}, true
	}

	names := make([]string, len(matched))
	for i, f := range matched {
		names[i] = f.Name
	}
	return &fieldAdaptor{entries: names}, true
}

func (a *fieldAdaptor) String() string {
	if a.field == nil {
		return ""
	}
	out, _ := a.field.Render(value.RenderOpts{})
	return ansi.Strip(out)
}

func (a *fieldAdaptor) Value() any {
	if a.field == nil {
		return nil
	}
	return a.field.Value
}

func (a *fieldAdaptor) Entries() []string {
	return a.entries
}

func (a *fieldAdaptor) compareValue(other string) (int, bool) {
	if a.field == nil || a.field.Value == nil {
		return 0, false
	}
	if t, ok := asTime(a.field.Value); ok {
		if parsed, err := wtime.ParseTime(other); err == nil {
			return value.Compare(t, parsed), true
		}
		if d, err := wtime.ParseDuration(other); err == nil {
			return value.Compare(time.Since(t), d), true
		}
		return 0, false
	}
	parsed, err := value.ParseNew([]string{other}, a.field.Value)
	if err != nil {
		return 0, false
	}
	return value.Compare(a.field.Value, parsed), true
}

func (a *fieldAdaptor) Equals(other string) (bool, bool) {
	result, ok := a.compareValue(other)
	return result == 0, ok
}

func (a *fieldAdaptor) Compare(other string) (int, bool) {
	if a.field == nil || !isOrderable(a.field.Value) {
		return 0, false
	}
	return a.compareValue(other)
}

func isOrderable(v any) bool {
	if v == nil {
		return false
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	if rv.Type().ConvertibleTo(reflect.TypeFor[time.Time]()) {
		return true
	}
	// A type with its own "Compare(T) int" method (matching the cmp.Compare
	// contract) already knows how to order itself, e.g. types.MeterUsage
	// orders by used/total ratio. value.Compare dispatches to it when
	// sorting; reuse that same detection here so filtering does too.
	return value.HasSelfCompare(rv.Interface())
}

func getSliceValue(value any) ([]string, bool) {
	if value == nil {
		return nil, false
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}
	result := make([]string, rv.Len())
	for i := range rv.Len() {
		elem := rv.Index(i)
		if s, ok := elem.Interface().(string); ok {
			result[i] = s
		} else if s, ok := elem.Interface().(fmt.Stringer); ok {
			result[i] = s.String()
		} else {
			result[i] = fmt.Sprintf("%v", elem.Interface())
		}
	}
	return result, true
}

func asTime(v any) (time.Time, bool) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return time.Time{}, false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || !rv.Type().ConvertibleTo(reflect.TypeFor[time.Time]()) {
		return time.Time{}, false
	}
	return rv.Convert(reflect.TypeFor[time.Time]()).Interface().(time.Time), true
}
