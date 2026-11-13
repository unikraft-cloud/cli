// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kong

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/alecthomas/kong"
)

// Optional is a mapper function that makes a field optional.
func Optional() kong.MapperFunc {
	// NOTE: this works with:
	// --optional
	// --optional=value
	// -ovalue
	// --optional --other-flag (does not consume --other-flag)
	// but NOT with:
	// --optional value
	// -o value
	// because the latter forms would be ambiguous with positional arguments.
	return func(ctx *kong.DecodeContext, target reflect.Value) error {
		switch tok := ctx.Scan.Peek(); tok.Type {
		case kong.FlagValueToken, kong.ShortFlagTailToken:
			r := kong.NewRegistry().RegisterDefaults()
			return r.ForValue(target).Decode(ctx, target)
		default:
			return nil
		}
	}
}

// HostEnv is a mapper function for a map[string]string of environment
// variables. A KEY with no "=" takes its value from the local environment.
func HostEnv() kong.MapperFunc {
	return func(ctx *kong.DecodeContext, target reflect.Value) error {
		var record string
		if err := ctx.Scan.PopValueInto("env", &record); err != nil {
			return err
		}
		key, val, ok := strings.Cut(record, "=")
		if key == "" {
			return fmt.Errorf(`expected "<key>[=<value>]" but got %q`, record)
		}
		if !ok {
			if val, ok = os.LookupEnv(key); !ok {
				return fmt.Errorf("%s is not set in the local environment, use %s= for an empty value", key, key)
			}
		}
		if target.IsNil() {
			target.Set(reflect.MakeMap(target.Type()))
		}
		target.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(val))
		return nil
	}
}
