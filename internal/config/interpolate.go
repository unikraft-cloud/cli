// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package config

import (
	"encoding"

	"mvdan.cc/sh/v3/shell"
)

// Interpolate is a string-like configuration value which supports shell-style
// environment interpolation.
//
// Values are expanded as if they were within double quotes. Command
// substitutions (e.g. $(cmd)) are not supported.
//
// The raw value is persisted as-is when saving the configuration.
type Interpolate[T any] struct {
	raw string

	expanded   string
	expandedOK bool
}

var (
	_ encoding.TextMarshaler   = (*Interpolate[string])(nil)
	_ encoding.TextUnmarshaler = (*Interpolate[string])(nil)
)

func InterpolateString(raw string) Interpolate[string] {
	return NewInterpolate[string](raw)
}

func NewInterpolate[T any](raw string) Interpolate[T] {
	return Interpolate[T]{raw: raw}
}

func (i *Interpolate[T]) SetRaw(raw string) {
	if i == nil {
		return
	}
	i.raw = raw
	i.expanded = ""
	i.expandedOK = false
}

func (i Interpolate[T]) Raw() string {
	return i.raw
}

func (i Interpolate[T]) IsZero() bool {
	return i.raw == ""
}

func (i Interpolate[T]) MarshalText() ([]byte, error) {
	return []byte(i.raw), nil
}

func (i *Interpolate[T]) UnmarshalText(text []byte) error {
	if i == nil {
		return nil
	}
	i.raw = string(text)
	i.expanded = ""
	i.expandedOK = false
	return nil
}

func (i *Interpolate[T]) String() string {
	if i == nil {
		return ""
	}
	if i.expandedOK {
		return i.expanded
	}
	out, err := shell.Expand(i.raw, nil)
	if err != nil {
		out = i.raw
	}
	i.expanded = out
	i.expandedOK = true
	return out
}
