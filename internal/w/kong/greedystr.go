// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kong

import (
	"fmt"

	"github.com/alecthomas/kong"
)

// GreedyString is a string type that consumes all arguments, including those
// starting with '-'.
type GreedyString string

// Decode implements kong.MapperValue.
func (h *GreedyString) Decode(ctx *kong.DecodeContext) error {
	t := ctx.Scan.Pop()
	if t.IsEOL() {
		return fmt.Errorf("missing value")
	}
	// otherwise, ignore the type, we consume it anyways

	switch v := t.Value.(type) {
	case string:
		*h = GreedyString(v)
	default:
		*h = GreedyString(fmt.Sprint(v))
	}

	return nil
}

// GreedyStrings is a string slice type that consumes all arguments, including those
// beginning with '-'.
type GreedyStrings []string

// Decode implements kong.MapperValue.
func (h *GreedyStrings) Decode(ctx *kong.DecodeContext) error {
	sep := ctx.Value.Tag.Sep

	t := ctx.Scan.Pop()
	if t.IsEOL() {
		tail := ""
		if sep != -1 {
			tail = string(sep) + "..."
		}
		return fmt.Errorf("missing value, expecting \"<arg>%s\"", tail)
	}
	// otherwise, ignore the type, we consume it anyways

	switch v := t.Value.(type) {
	case string:
		if sep == -1 {
			*h = append(*h, v)
			return nil
		}
		for _, part := range kong.SplitEscaped(v, sep) {
			*h = append(*h, part)
		}
	case []any:
		for _, part := range v {
			*h = append(*h, fmt.Sprint(part))
		}
	default:
		*h = append(*h, fmt.Sprint(v))
	}

	return nil
}
