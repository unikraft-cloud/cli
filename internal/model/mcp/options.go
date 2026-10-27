// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcp

// ToolOption adjusts how a resource type's tools are registered.
type ToolOption func(*toolOptions)

type toolOptions struct {
	createDefaults map[string]any
	editNote       string
}

func newToolOptions(opts []ToolOption) toolOptions {
	var o toolOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithCreateDefault makes the create tool set the field at path to value
// unless the caller sets it.
func WithCreateDefault(path string, value any) ToolOption {
	return func(o *toolOptions) {
		if o.createDefaults == nil {
			o.createDefaults = map[string]any{}
		}
		o.createDefaults[path] = value
	}
}

// WithEditNote appends note to the edit tool's description.
func WithEditNote(note string) ToolOption {
	return func(o *toolOptions) {
		o.editNote = note
	}
}
