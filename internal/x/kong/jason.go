// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kong

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/alecthomas/kong"

	"unikraft.com/cli/internal/jason"
)

// Jason is a mapper function that decodes a flag from jason items, so that
// `--flag a=1 b=2` works as well as `--flag a=1,b=2`.
//
// NOTE: every following argument that is a jason item is consumed, which is
// unambiguous since names, IDs and flags never are. A lone CSV item
// (`key=value` holding a comma), or a value that isn't an item at all, is
// decoded as it would be without this mapper.
//
// Each use of a slice flag appends one element, and of a map flag adds its
// entries, as without this mapper.
func Jason() kong.MapperFunc {
	return func(ctx *kong.DecodeContext, target reflect.Value) error {
		first := ctx.Scan.Pop()
		if first.IsEOL() {
			return fmt.Errorf("missing value")
		}
		item, err := jason.ParseItem(fmt.Sprint(first.Value))
		if err != nil {
			return decodeDefault(ctx, target, first)
		}

		items := append([]jason.Item{item}, ScanItems(ctx)...)
		if len(items) == 1 && isCSVItem(items[0]) {
			return decodeDefault(ctx, target, first)
		}
		return decodeItems(items, target)
	}
}

func decodeItems(items []jason.Item, target reflect.Value) error {
	switch target.Kind() {
	case reflect.Slice:
		elem := reflect.New(target.Type().Elem())
		if err := jason.UnmarshalItems(items, elem.Interface()); err != nil {
			return err
		}
		target.Set(reflect.Append(target, elem.Elem()))
	case reflect.Map:
		entries := reflect.New(target.Type())
		if err := jason.UnmarshalItems(items, entries.Interface()); err != nil {
			return err
		}
		if target.IsNil() {
			target.Set(reflect.MakeMap(target.Type()))
		}
		for k, v := range entries.Elem().Seq2() {
			target.SetMapIndex(k, v)
		}
	default:
		target.Set(reflect.Zero(target.Type()))
		return jason.UnmarshalItems(items, target.Addr().Interface())
	}
	return nil
}

// ScanItems consumes every following argument that is a jason item, stopping
// at the first flag, "--", or argument that isn't one.
func ScanItems(ctx *kong.DecodeContext) []jason.Item {
	var items []jason.Item
	for {
		next := ctx.Scan.Peek()
		s, ok := next.Value.(string)
		if next.Type != kong.UntypedToken || !ok || strings.HasPrefix(s, "-") {
			return items
		}
		item, err := jason.ParseItem(s)
		if err != nil {
			return items
		}
		ctx.Scan.Pop()
		items = append(items, item)
	}
}

// isCSVItem reports whether item is a plain `key=value` whose value holds a
// comma, so is the type's own comma-separated text form.
func isCSVItem(item jason.Item) bool {
	return !item.Raw && len(item.Path) == 1 && item.Path[0] != "" && strings.Contains(item.Value, ",")
}

func decodeDefault(ctx *kong.DecodeContext, target reflect.Value, token kong.Token) error {
	ctx.Scan.PushToken(token)
	// HACK: kong's map decoder reads type:"<key>:<value>" from the tag, which
	// here names this mapper instead.
	value := *ctx.Value
	tag := *value.Tag
	tag.Type = ""
	value.Tag = &tag
	ctx = &kong.DecodeContext{Value: &value, Scan: ctx.Scan}
	return kong.NewRegistry().RegisterDefaults().ForValue(target).Decode(ctx, target)
}
