// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInterpolateStringExpandEnv(t *testing.T) {
	t.Setenv("UKC_TOKEN", "s3cr3t")

	v := InterpolateString("${UKC_TOKEN}")
	require.Equal(t, "${UKC_TOKEN}", v.Raw())
	require.Equal(t, "s3cr3t", v.String())
}

func TestInterpolateStringInvalidSyntaxFallsBackToRaw(t *testing.T) {
	v := InterpolateString("${")
	require.Equal(t, "${", v.String())
}

func TestInterpolateStringCachesExpandedValue(t *testing.T) {
	t.Setenv("UKC_TOKEN", "first")

	v := InterpolateString("${UKC_TOKEN}")
	require.Equal(t, "first", v.String())

	// Changing the environment after the first expansion should not affect
	// subsequent calls, since values are cached.
	t.Setenv("UKC_TOKEN", "second")
	require.Equal(t, "first", v.String())
}
