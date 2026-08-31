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

// HACK: splitTopLevel splits s on commas, skipping those inside a JSON value. Only a
// value that opens with {, [ or " immediately after its key's "=" is treated as
// JSON, so a brace or quote appearing mid-value stays literal and the item
// separators around it still split.
func splitTopLevel(s string) ([]string, error) {
	var parts []string
	var open []byte
	start := 0
	valuePos := -1
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]

		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}

		if len(open) > 0 {
			switch c {
			case '"':
				inStr = true
			case '{':
				open = append(open, '}')
			case '[':
				open = append(open, ']')
			case '}', ']':
				if open[len(open)-1] == c {
					open = open[:len(open)-1]
				}
			}
			continue
		}

		switch {
		case c == ',':
			parts = append(parts, s[start:i])
			start = i + 1
			valuePos = -1
		case c == '=' && valuePos < 0:
			valuePos = i + 1
		case i != valuePos:
		case c == '{':
			open = append(open, '}')
		case c == '[':
			open = append(open, ']')
		case c == '"':
			inStr = true
		}
	}
	if inStr {
		return nil, fmt.Errorf("unterminated quote in %q", s)
	}
	if len(open) > 0 {
		return nil, fmt.Errorf("missing %q in %q", string(open[len(open)-1]), s)
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts, nil
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

			// Each input string is exactly one element, passed on verbatim.
			// Use repeated flags for multiple elements.
			val := reflect.New(elemType).Elem()
			if err := parseReflect([]string{input}, val); err != nil {
				return err
			}
			slice = reflect.Append(slice, val)
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

			// One key=value entry per input; multiple entries come from
			// repeated inputs, never from splitting one. The value reaches
			// the element type unsplit, so a string element keeps commas and
			// further "=" verbatim. An entry with no key is skipped.
			k, v, _ := strings.Cut(input, "=")
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			key := reflect.New(output.Type().Key()).Elem()
			if err := parseReflect([]string{k}, key); err != nil {
				return err
			}
			val := reflect.New(output.Type().Elem()).Elem()
			if err := parseReflect([]string{v}, val); err != nil {
				return err
			}
			mapp.SetMapIndex(key, val)
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

		notFound := make(map[string]struct{})
		for _, input := range input {
			items, err := splitTopLevel(input)
			if err != nil {
				return err
			}
		process:
			for _, item := range items {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				k, v, _ := strings.Cut(item, "=")

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
