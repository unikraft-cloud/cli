// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package value

import (
	"cmp"
	"fmt"
	"reflect"
	"time"
)

// IsZero reports whether v is a zero value for its type.
func IsZero(v any) bool {
	if v == nil {
		return true
	}
	return reflect.ValueOf(v).IsZero()
}

// Compare compares two values in a type-aware manner.
//
// It returns -1 if a < b, 0 if a == b, and 1 if a > b.
//
// When possible, it performs type-specific or kind-based comparisons (including
// for numeric, string, bool, and time-related types, even if they are different
// named types with compatible underlying kinds). If it cannot perform a
// type-aware comparison, it falls back to comparing the string representations
// of the values.
func Compare(a, b any) int {
	// Handle nil cases
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}

	if result, ok := compareSelf(a, b); ok {
		return result
	}

	// Try type-specific comparisons
	switch av := a.(type) {
	case time.Time:
		if bv, ok := b.(time.Time); ok {
			return av.Compare(bv)
		}
	case *time.Time:
		if bv, ok := b.(*time.Time); ok {
			if av == nil && bv == nil {
				return 0
			}
			if av == nil {
				return -1
			}
			if bv == nil {
				return 1
			}
			return av.Compare(*bv)
		}
	case time.Duration:
		if bv, ok := b.(time.Duration); ok {
			return cmp.Compare(av, bv)
		}
	case int:
		if bv, ok := b.(int); ok {
			return cmp.Compare(av, bv)
		}
	case int8:
		if bv, ok := b.(int8); ok {
			return cmp.Compare(av, bv)
		}
	case int16:
		if bv, ok := b.(int16); ok {
			return cmp.Compare(av, bv)
		}
	case int32:
		if bv, ok := b.(int32); ok {
			return cmp.Compare(av, bv)
		}
	case int64:
		if bv, ok := b.(int64); ok {
			return cmp.Compare(av, bv)
		}
	case uint:
		if bv, ok := b.(uint); ok {
			return cmp.Compare(av, bv)
		}
	case uint8:
		if bv, ok := b.(uint8); ok {
			return cmp.Compare(av, bv)
		}
	case uint16:
		if bv, ok := b.(uint16); ok {
			return cmp.Compare(av, bv)
		}
	case uint32:
		if bv, ok := b.(uint32); ok {
			return cmp.Compare(av, bv)
		}
	case uint64:
		if bv, ok := b.(uint64); ok {
			return cmp.Compare(av, bv)
		}
	case float32:
		if bv, ok := b.(float32); ok {
			return cmp.Compare(av, bv)
		}
	case float64:
		if bv, ok := b.(float64); ok {
			return cmp.Compare(av, bv)
		}
	case string:
		if bv, ok := b.(string); ok {
			return cmp.Compare(av, bv)
		}
	case bool:
		if bv, ok := b.(bool); ok {
			// false < true
			if av == bv {
				return 0
			}
			if !av && bv {
				return -1
			}
			return 1
		}
	}

	// Try reflection-based comparison for underlying types
	if result, ok := compareReflect(a, b); ok {
		return result
	}

	// Fall back to string comparison
	aStr, err := Render(a, RenderOpts{})
	if err != nil {
		aStr = fmt.Sprint(a)
	}
	bStr, err := Render(b, RenderOpts{})
	if err != nil {
		bStr = fmt.Sprint(b)
	}
	return cmp.Compare(aStr, bStr)
}

// compareSelf detects and invokes a "Compare(T) int" method on a's type,
// where T is the same type as a, matching the cmp.Compare contract.
func compareSelf(a, b any) (int, bool) {
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)
	if av.Type() != bv.Type() {
		return 0, false
	}
	if !hasSelfCompareMethod(av) {
		return 0, false
	}

	method := av.MethodByName("Compare")
	return int(method.Call([]reflect.Value{bv})[0].Int()), true
}

// HasSelfCompare reports whether v has a "Compare(T) int" method where T is
// v's own type, matching the cmp.Compare contract. Compare uses this same
// detection internally (see compareSelf) to dispatch to a type's own
// ordering; callers that need to know upfront whether such ordering exists
// (e.g. to decide whether a value can be used with an ordering filter) can
// check it directly.
func HasSelfCompare(v any) bool {
	if v == nil {
		return false
	}
	return hasSelfCompareMethod(reflect.ValueOf(v))
}

func hasSelfCompareMethod(rv reflect.Value) bool {
	method := rv.MethodByName("Compare")
	if !method.IsValid() {
		return false
	}
	mt := method.Type()
	return mt.NumIn() == 1 && mt.In(0) == rv.Type() && mt.NumOut() == 1 && mt.Out(0).Kind() == reflect.Int
}

// compareReflect attempts to compare values using reflection.
// It handles cases where the underlying type is comparable but wrapped in a custom type.
func compareReflect(a, b any) (int, bool) {
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)

	// Dereference pointers
	for av.Kind() == reflect.Pointer && !av.IsNil() {
		av = av.Elem()
	}
	for bv.Kind() == reflect.Pointer && !bv.IsNil() {
		bv = bv.Elem()
	}

	// Handle pointer nil cases after dereferencing
	if av.Kind() == reflect.Pointer && av.IsNil() {
		if bv.Kind() == reflect.Pointer && bv.IsNil() {
			return 0, true
		}
		return -1, true
	}
	if bv.Kind() == reflect.Pointer && bv.IsNil() {
		return 1, true
	}

	// Check if underlying type is time.Time
	if av.Type().ConvertibleTo(reflect.TypeFor[time.Time]()) && bv.Type().ConvertibleTo(reflect.TypeFor[time.Time]()) {
		at := av.Convert(reflect.TypeFor[time.Time]()).Interface().(time.Time)
		bt := bv.Convert(reflect.TypeFor[time.Time]()).Interface().(time.Time)
		return at.Compare(bt), true
	}

	// Check if underlying type is time.Duration
	if av.Type().ConvertibleTo(reflect.TypeFor[time.Duration]()) && bv.Type().ConvertibleTo(reflect.TypeFor[time.Duration]()) {
		ad := av.Convert(reflect.TypeFor[time.Duration]()).Interface().(time.Duration)
		bd := bv.Convert(reflect.TypeFor[time.Duration]()).Interface().(time.Duration)
		return cmp.Compare(ad, bd), true
	}

	// Handle numeric types with same underlying kind
	if av.Kind() == bv.Kind() {
		switch av.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return cmp.Compare(av.Int(), bv.Int()), true
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return cmp.Compare(av.Uint(), bv.Uint()), true
		case reflect.Float32, reflect.Float64:
			return cmp.Compare(av.Float(), bv.Float()), true
		case reflect.String:
			return cmp.Compare(av.String(), bv.String()), true
		case reflect.Bool:
			ab, bb := av.Bool(), bv.Bool()
			if ab == bb {
				return 0, true
			}
			if !ab && bb {
				return -1, true
			}
			return 1, true
		}
	}

	return 0, false
}
