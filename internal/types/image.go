// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"strings"

	"github.com/distribution/reference"

	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/value"
)

// ImageRef is a generic wrapper around a Docker image reference.
type ImageRef[T interface {
	reference.Named
	comparable
}] struct {
	Reference T
}

func (ir ImageRef[T]) MarshalText() ([]byte, error) {
	var zero T
	if ir.Reference == zero {
		return []byte{}, nil
	}
	s := images.FamiliarString(ir.Reference)
	return []byte(s), nil
}

// Render implements value.Renderer. In short form (e.g. table output), the
// digest is elided to keep output concise; in long form (e.g. detail views,
// JSON/YAML output via MarshalText) the full canonical reference, including
// any digest, is shown.
func (ir ImageRef[T]) Render(opts value.RenderOpts) (string, error) {
	var zero T
	if ir.Reference == zero {
		return "", nil
	}
	s := images.FamiliarString(ir.Reference)
	if opts.Short {
		s, _, _ = strings.Cut(s, "@")
	}
	return s, nil
}

func (ir ImageRef[T]) Value() any {
	return ir
}

func (ir *ImageRef[T]) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		var zero T
		ir.Reference = zero
		return nil
	}
	ref, err := images.ParseNormalizedNamed(string(text))
	if err != nil {
		return err
	}
	ref = reference.TagNameOnly(ref)
	ir.Reference = ref.(T)
	return nil
}

func (ir ImageRef[T]) Link() (string, resource.Key, bool) {
	var zero T
	if ir.Reference == zero {
		return "", nil, false
	}
	return "image", multimetro.Key{
		Name: ir.Reference.String(),
	}, false
}
