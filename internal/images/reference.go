// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import (
	"github.com/distribution/reference"

	"unikraft.com/cli/internal/config"
	xreference "unikraft.com/cli/internal/x/reference"
)

const DefaultRegistry = "unikraft.io"

// ParseNormalizedNamed parses an image reference against the default registry.
func ParseNormalizedNamed(key string) (reference.Named, error) {
	return ParseNormalizedNamedMetro(nil, key)
}

// ParseNormalizedNamedMetro parses an image reference against the metro's
// registry, or the default registry when metro is nil.
func ParseNormalizedNamedMetro(metro *config.Metro, key string) (reference.Named, error) {
	key, err := stripImageURI(key)
	if err != nil {
		return nil, err
	}

	index := DefaultRegistry
	if metro != nil {
		index = metro.Index().Host
	}
	return xreference.ParseNormalizedNamed(
		key,
		xreference.WithDefaultDomain(index),
		xreference.WithDefaultPrefix("official/"),
	)
}

// FamiliarString renders a reference without the default registry and prefix.
func FamiliarString(ref reference.Reference) string {
	return xreference.FamiliarString(
		ref,
		xreference.WithDefaultDomain(DefaultRegistry),
		xreference.WithDefaultPrefix("official/"),
	)
}
