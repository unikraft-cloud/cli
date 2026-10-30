// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/tui/meter"
)

func TestGaugeRender(t *testing.T) {
	t.Setenv(meter.WidthEnv, "5")

	g := Gauge[SizeMebibytes]{Current: 640, Min: 256, Max: 1024}
	out, err := g.Render(value.RenderOpts{})
	require.NoError(t, err)
	assert.Equal(t, "640MiB ┣━╋─┤ 256MiB...1GiB", ansi.Strip(out))

	out, err = g.Render(value.RenderOpts{Short: true})
	require.NoError(t, err)
	assert.Equal(t, "640MiB", out)

	out, err = Gauge[int]{Current: 2}.Render(value.RenderOpts{})
	require.NoError(t, err)
	assert.Equal(t, "2", out)
}

func TestGaugeText(t *testing.T) {
	var mem Gauge[SizeMebibytes]
	require.NoError(t, mem.UnmarshalText([]byte("1GiB")))
	assert.Equal(t, Gauge[SizeMebibytes]{Current: 1024}, mem)

	var vcpus Gauge[int]
	require.NoError(t, vcpus.UnmarshalText([]byte("3")))
	assert.Equal(t, Gauge[int]{Current: 3}, vcpus)

	text, err := Gauge[int]{Current: 3, Min: 1, Max: 4}.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "3", string(text))

	assert.Equal(t, -1, Gauge[int]{Current: 1, Max: 9}.Compare(Gauge[int]{Current: 2}))
}
