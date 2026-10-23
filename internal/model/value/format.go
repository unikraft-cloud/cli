// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package value

import (
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ettle/strcase"
)

// RenderOpts controls how a value is rendered to a string via Render.
type RenderOpts struct {
	// Short indicates whether the value should be rendered in a more
	// concise form suitable for table/short output (e.g. eliding an image
	// digest), rather than with full detail. The zero value renders with
	// full detail.
	Short bool

	// Quiet indicates the value is being rendered for quiet/scriptable
	// output, where a plain, minimal representation (e.g. a raw percentage
	// rather than a colored meter bar) is preferred over a rich
	// human-readable one.
	Quiet bool
}

// Renderer is implemented by types that want to customize their textual
// representation based on the requested verbosity, e.g. hiding an image
// digest when rendered in short/table form, but including it in long/detail
// form.
type Renderer interface {
	Render(opts RenderOpts) (string, error)
}

func Render(value any, opt RenderOpts) (string, error) {
	if value == nil {
		return "", nil
	}

	// Check for nil pointers before checking for interfaces
	// because a nil pointer to a type that implements an interface
	// will pass the interface check but panic when calling methods
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return "", nil
	}

	if value, ok := value.(Renderer); ok {
		return value.Render(opt)
	}
	if value, ok := value.(fmt.Stringer); ok {
		return value.String(), nil
	}
	if value, ok := value.(encoding.TextMarshaler); ok {
		dt, err := value.MarshalText()
		return string(dt), err
	}

	switch v.Kind() {
	case reflect.Pointer:
		return Render(v.Elem().Interface(), opt)
	case reflect.String:
		return v.String(), nil
	case reflect.Slice, reflect.Array:
		var result []string
		for i := range v.Len() {
			val := v.Index(i)
			valStr, err := Render(val.Interface(), opt)
			if err != nil {
				return "", err
			}
			if val.Kind() == reflect.String {
				valStr = strconv.Quote(valStr)
			}
			result = append(result, valStr)
		}
		if len(result) == 0 {
			return "", nil
		}
		return "[" + strings.Join(result, ", ") + "]", nil
	case reflect.Map:
		var result []string
		for _, key := range v.MapKeys() {
			val := v.MapIndex(key)
			keyStr, err := Render(key.Interface(), opt)
			if err != nil {
				return "", err
			}
			valStr, err := Render(val.Interface(), opt)
			if err != nil {
				return "", err
			}
			result = append(result, fmt.Sprintf("%s=%s", keyStr, valStr))
		}
		slices.Sort(result)
		return strings.Join(result, ", "), nil
	case reflect.Struct:
		var result []string
		for i := range v.NumField() {
			field := v.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			val := v.Field(i)
			valStr, err := Render(val.Interface(), opt)
			if err != nil {
				return "", err
			}
			if valStr == "" {
				continue
			}
			// Use "name" tag for value formatting, separate from "field" tag
			// This allows fields to be excluded from the field system (field:"-")
			// while still being formattable for --set values
			name := field.Tag.Get("name")
			if name == "-" {
				continue
			}
			if name == "" {
				name = strcase.ToKebab(field.Name)
			}
			result = append(result, fmt.Sprintf("%s=%s", name, valStr))
		}
		return strings.Join(result, ", "), nil
	default:
		return fmt.Sprintf("%v", value), nil
	}
}
