// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package jason

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
)

// Visualize returns a human-readable tree representation of the parsed
// value. It uses box-drawing characters to show nesting structure.
//
// Example output for a Config value:
//
//	{
//		name = "HTTPie"
//		stars = 54000
//		apps = [
//			[0] = "Terminal"
//			[1] = "Desktop"
//			[2] = "Web"
//		]
//	}
func (n *Jason[T]) Visualize() string {
	var b strings.Builder
	visualize(&b, n.Value, 0)
	return b.String()
}

// visualize writes a human-readable tree representation of the value
// into the strings.Builder at the given indentation depth. Maps are shown
// as { key = value ... }, slices as [ index = value ... ], strings are
// quoted, and other values are formatted with %v.
func visualize(b *strings.Builder, v any, depth int) {
	indent := strings.Repeat("  ", depth)
	switch val := v.(type) {
	case map[string]any:
		fmt.Fprint(b, "{\n")
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, "%s  %s = ", indent, k)
			visualize(b, val[k], depth+1)
			fmt.Fprint(b, "\n")
		}
		fmt.Fprintf(b, "%s}", indent)
	case []any:
		fmt.Fprint(b, "[\n")
		for i, elem := range val {
			fmt.Fprintf(b, "%s  [%d] = ", indent, i)
			visualize(b, elem, depth+1)
			fmt.Fprint(b, "\n")
		}
		fmt.Fprintf(b, "%s]", indent)
	case string:
		fmt.Fprint(b, strconv.Quote(val))
	case nil:
		fmt.Fprint(b, "null")
	default:
		fmt.Fprintf(b, "%v", val)
	}
}

// debugTree returns a detailed multi-line breakdown showing how each
// input item is parsed and where it lands in the final tree. This is
// useful for understanding the parsing of complex nested expressions.
//
// The output shows every item grouped by its top-level key, with parsed
// path segments and the final assigned value.
func debugTree(items []string) (string, error) {
	parsed := make([]Item, 0, len(items))
	hasArrayRoot := false
	for _, item := range items {
		p, err := ParseItem(item)
		if err != nil {
			return "", err
		}
		if len(p.Path) > 0 && p.Path[0] == "" {
			hasArrayRoot = true
		}
		parsed = append(parsed, p)
	}

	var b strings.Builder
	fmt.Fprint(&b, "Input items:\n")
	for i, p := range parsed {
		pathStr := strings.Join(p.Path, "][")
		if pathStr != "" {
			pathStr = "[" + pathStr + "]"
		}
		op := "="
		if p.Raw {
			op = ":="
		}
		fmt.Fprintf(&b, "  %d: %s%s%s  → path=%v  isRaw=%v  value=%q\n",
			i, pathStr, op, p.Value, p.Path, p.Raw, p.Value)
	}

	fmt.Fprintf(&b, "\nRoot is array: %v\n\n", hasArrayRoot)

	var root any
	if hasArrayRoot {
		root = make([]any, 0)
	} else {
		root = make(map[string]any)
	}

	fmt.Fprint(&b, "Assignments (in order):\n")
	for _, p := range parsed {
		var val any
		if p.Raw {
			if err := json.Unmarshal([]byte(p.Value), &val); err != nil {
				return "", fmt.Errorf("invalid raw JSON value %q: %w", p.Value, err)
			}
		} else {
			val = p.Value
		}
		path := p.Path
		if len(path) > 0 && path[0] == "" {
			path = path[1:]
		}

		if len(path) == 0 {
			if m, ok := val.(map[string]any); ok {
				rootMap, ok := root.(map[string]any)
				if !ok {
					root = make(map[string]any)
					rootMap = root.(map[string]any)
				}
				maps.Copy(rootMap, m)
			} else {
				root = val
			}
			fmt.Fprintf(&b, "  path=[root]  val=%#v\n", val)
			continue
		}

		pathStr := strings.Join(path, " → ")
		var err error
		root, err = assignAtPath(root, path, val)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "  path=[%s]  val=%#v\n", pathStr, val)
	}

	fmt.Fprint(&b, "\nFinal tree:\n")
	visualize(&b, root, 0)
	fmt.Fprint(&b, "\n")

	data, _ := json.MarshalIndent(root, "", "  ")
	fmt.Fprintf(&b, "\nJSON:\n%s\n", data)

	return b.String(), nil
}
