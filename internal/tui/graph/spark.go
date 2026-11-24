// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package graph

import (
	"math"
	"strings"
)

var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// Sparkline draws a series as one row of block glyphs, width cells wide. The
// newest point is at the right edge. Values are scaled to ceiling, so several
// rows can share one scale. A gap is a blank cell, and a real zero is the
// lowest block.
func Sparkline(points []Point, width int, ceiling float64) string {
	if width <= 0 {
		return ""
	}
	points = points[max(0, len(points)-width):]
	var sb strings.Builder
	sb.WriteString(strings.Repeat(" ", width-len(points)))
	for _, p := range points {
		if !p.OK {
			sb.WriteRune(' ')
			continue
		}
		level := 0
		if ceiling > 0 {
			level = int(math.Round(min(max(p.V/ceiling, 0), 1) * float64(len(sparkBlocks)-1)))
		}
		sb.WriteRune(sparkBlocks[level])
	}
	return sb.String()
}

// Ceiling returns a shared scale for series that are drawn as sparklines.
func Ceiling(series ...[]Point) float64 {
	m := 0.0
	for _, points := range series {
		m = max(m, maxValue(points))
	}
	return m
}
