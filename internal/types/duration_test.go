// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/types"
)

// TestDurationSRoundsUp checks that a sub-second duration never collapses to
// zero.
func TestDurationSRoundsUp(t *testing.T) {
	for _, tt := range []struct {
		text string
		want types.DurationS
	}{
		{"0", 0},
		{"5", 5},
		{"-1", -1},
		{"0s", 0},
		{"1s", 1},
		{"1ms", 1},
		{"200ms", 1},
		{"999ms", 1},
		{"1500ms", 2},
		{"2s", 2},
		{"1m", 60},
		{"1m30s", 90},
		{"-1s", -1},
		{"-200ms", -1},
	} {
		t.Run(tt.text, func(t *testing.T) {
			var got types.DurationS
			require.NoError(t, got.UnmarshalText([]byte(tt.text)))
			assert.Equal(t, tt.want, got)
		})
	}
}
