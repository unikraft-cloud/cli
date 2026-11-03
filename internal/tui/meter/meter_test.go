// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package meter

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestGauge(t *testing.T) {
	tests := []struct {
		ratio float64
		width int
		want  string
	}{
		{0, 5, "╋───┤"},
		{0.5, 5, "┣━╋─┤"},
		{1, 5, "┣━━━╋"},
		{0.74, 5, "┣━━╋┤"},
		{-1, 5, "╋───┤"},
		{2, 5, "┣━━━╋"},
		{0.5, 1, "╋"},
	}
	for _, tt := range tests {
		got := ansi.Strip(Gauge(tt.ratio, tt.width, DefaultThresholds))
		assert.Equal(t, tt.want, got, "ratio %v width %d", tt.ratio, tt.width)
	}
}
