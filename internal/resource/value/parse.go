// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package value

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/ettle/strcase"

	xmaps "unikraft.com/cli/internal/x/maps"
)

// splitTopLevel splits s on commas, keeping those inside balanced {} or []
// pairs and inside JSON string literals, so a value carrying JSON survives.
func splitTopLevel(s string) []string {
	var parts []string
	var buf strings.Builder
	depth := 0
	inStr := false
	esc := false
	for _, r := range s {
		if inStr {
			buf.WriteRune(r)
			if esc {
				esc = false
			} else if r == '\\' {
				esc = true
			} else if r == '"' {
				inStr = false
			}
			continue
		}
		switch r {
		case '"':
			inStr = true
			buf.WriteRune(r)
		case '{', '[':
			depth++
			buf.WriteRune(r)
		case '}', ']':
			depth--
			buf.WriteRune(r)
		case ',':
			if depth == 0 {
				parts = append(parts, buf.String())
				buf.Reset()
			} else {
				buf.WriteRune(r)
			}
		default:
			buf.WriteRune(r)
		}
	}
	if buf.Len() > 0 {
		parts = append(parts, buf.String())
	}
	return parts
}

func Parse[T any](input []string) (T, error) {
	var t T
	output, err := ParseNew(input, t)
	if err != nil {
		return t, err
	}
	return output.(T), nil
}

func ParseNew(input []string, output any) (any, error) {
	parsedVal, err := parseNewReflect(input, reflect.ValueOf(output))
	if err != nil {
		return nil, err
	}
	return parsedVal.Interface(), nil
}

func parseNewReflect(input []string, output reflect.Value) (reflect.Value, error) {
	newVal := reflect.New(output.Type()).Elem()
	err := parseReflect(input, newVal)
	if err != nil {
		return reflect.Value{}, err
	}
	return newVal, nil
}

func parseReflect(input []string, value reflect.Value) error {
	if input == nil {
		return nil
	}

	output := value
	for output.Kind() == reflect.Pointer {
		if output.IsNil() {
			output.Set(reflect.New(output.Type().Elem()))
		}
		output = output.Elem()
	}

	if len(input) == 0 {
		output.Set(reflect.Zero(output.Type()))
		return nil
	}

	text, ok := value.Interface().(encoding.TextUnmarshaler)
	if ok {
		return text.UnmarshalText([]byte(input[0]))
	}
	if value.CanAddr() {
		textPtr, ok := value.Addr().Interface().(encoding.TextUnmarshaler)
		if ok {
			return textPtr.UnmarshalText([]byte(input[0]))
		}
	}

	switch output.Kind() {
	case reflect.String:
		output.SetString(input[0])
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(input[0], 10, output.Type().Bits())
		if err != nil {
			return err
		}
		output.SetInt(v)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(input[0], 10, output.Type().Bits())
		if err != nil {
			return err
		}
		output.SetUint(v)
		return nil
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(input[0], output.Type().Bits())
		if err != nil {
			return err
		}
		output.SetFloat(v)
		return nil
	case reflect.Bool:
		v, err := strconv.ParseBool(input[0])
		if err != nil {
			return err
		}
		output.SetBool(v)
		return nil
	case reflect.Slice:
		slice := reflect.MakeSlice(output.Type(), 0, 0)

		elemType := output.Type().Elem()

		// Check if the element type implements TextUnmarshaler. If so, each
		// input string is a complete element (don't comma-split), since the
		// element's own encoding may use commas internally.
		checkType := elemType
		if checkType.Kind() == reflect.Pointer {
			checkType = checkType.Elem()
		}
		_, elemIsTextUnmarshaler := reflect.New(checkType).Interface().(encoding.TextUnmarshaler)

		for _, input := range input {
			// Try JSON array syntax first.
			trimmed := strings.TrimSpace(input)
			if strings.HasPrefix(trimmed, "[") {
				target := reflect.New(output.Type())
				if err := json.Unmarshal([]byte(trimmed), target.Interface()); err == nil {
					slice = reflect.AppendSlice(slice, target.Elem())
					continue
				}
			}

			if elemIsTextUnmarshaler {
				// Element handles its own parsing; pass the whole input
				// as a single element since the element's encoding may
				// use commas internally. Use repeated flags for multiple
				// elements.
				val := reflect.New(elemType).Elem()
				if err := parseReflect([]string{input}, val); err != nil {
					return err
				}
				slice = reflect.Append(slice, val)
				continue
			}

			// Fall back to comma-separated parsing.
			for _, item := range splitTopLevel(input) {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				val := reflect.New(elemType).Elem()
				err := parseReflect([]string{item}, val)
				if err != nil {
					return err
				}
				slice = reflect.Append(slice, val)
			}
		}
		output.Set(slice)
		return nil
	case reflect.Map:
		mapp := reflect.MakeMap(output.Type())
		for _, input := range input {
			// Try JSON object syntax first.
			trimmed := strings.TrimSpace(input)
			if strings.HasPrefix(trimmed, "{") {
				target := reflect.New(output.Type())
				if err := json.Unmarshal([]byte(trimmed), target.Interface()); err == nil {
					iter := target.Elem().MapRange()
					for iter.Next() {
						mapp.SetMapIndex(iter.Key(), iter.Value())
					}
					continue
				}
			}
			// Fall back to comma-separated key=value parsing.
			for _, item := range splitTopLevel(input) {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				k, v, _ := strings.Cut(item, "=")
				k = strings.TrimSpace(k)
				v = strings.TrimSpace(v)
				key := reflect.New(output.Type().Key()).Elem()
				err := parseReflect([]string{k}, key)
				if err != nil {
					return err
				}
				val := reflect.New(output.Type().Elem()).Elem()
				err = parseReflect([]string{v}, val)
				if err != nil {
					return err
				}
				mapp.SetMapIndex(key, val)
			}
		}
		output.Set(mapp)
		return nil
	case reflect.Struct:
		s := reflect.New(output.Type()).Elem()

		// Try JSON object syntax if single input starting with {.
		if len(input) == 1 {
			trimmed := strings.TrimSpace(input[0])
			if strings.HasPrefix(trimmed, "{") {
				target := reflect.New(output.Type())
				if err := json.Unmarshal([]byte(trimmed), target.Interface()); err == nil {
					output.Set(target.Elem())
					return nil
				}
			}
		}

		// Fall back to comma-separated key=value parsing.
		notFound := make(map[string]struct{})
		for _, input := range input {
		process:
			for _, item := range splitTopLevel(input) {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				k, v, _ := strings.Cut(item, "=")
				k = strings.TrimSpace(k)
				v = strings.TrimSpace(v)

				for i := range s.NumField() {
					field := s.Type().Field(i)
					if !field.IsExported() {
						continue
					}
					// Use "name" tag for value parsing, separate from "field" tag
					// This allows fields to be excluded from the field system (field:"-")
					// while still being parseable for --set values
					name := field.Tag.Get("name")
					if name == "-" {
						continue
					}
					if name == "" {
						name = field.Name
						name = strcase.ToKebab(name)
					}
					if k == name {
						fieldVal := s.Field(i)
						err := parseReflect([]string{v}, fieldVal)
						if err != nil {
							return err
						}
						continue process
					}
				}
				notFound[k] = struct{}{}
			}
		}
		if len(notFound) > 0 {
			return fmt.Errorf("unknown fields: %v", xmaps.OrderedKeys(notFound))
		}
		output.Set(s)
		return nil
	default:
		return fmt.Errorf("unsupported type: %T", value.Interface())
	}
}
