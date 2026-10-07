// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package platforms extends containerd's platform matchers.
package platforms

import (
	"slices"
	"strings"

	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// OnlyArch returns a matcher for platforms of any of the given architectures,
// whatever their OS. Platforms sort in the order their architectures were
// given.
func OnlyArch(arches ...string) platforms.MatchComparer {
	m := archMatcher{arches: arches, normalized: make([]string, len(arches))}
	for i, arch := range arches {
		m.normalized[i] = normalizeArch(arch)
	}
	return m
}

type archMatcher struct {
	arches     []string
	normalized []string
}

func (m archMatcher) Match(p ocispec.Platform) bool {
	return m.rank(p) < len(m.normalized)
}

func (m archMatcher) Less(a, b ocispec.Platform) bool {
	return m.rank(a) < m.rank(b)
}

func (m archMatcher) String() string {
	return strings.Join(m.arches, ", ")
}

func (m archMatcher) rank(p ocispec.Platform) int {
	if i := slices.Index(m.normalized, normalizeArch(p.Architecture)); i >= 0 {
		return i
	}
	return len(m.normalized)
}

func normalizeArch(arch string) string {
	return platforms.Normalize(ocispec.Platform{Architecture: arch}).Architecture
}
