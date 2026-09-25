// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package meter renders a colored horizontal bar representing a ratio within
// [0, 1], e.g. "⣿⣿⣿⣿⣿⣿⣿⣿⣀⣀", for displaying quota/usage-style values.
package meter

import (
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"unikraft.com/x/colors"
	"unikraft.com/x/guesstermwidth"
)

// MinWidth and MaxWidth bound the width returned by Width.
const (
	MinWidth = 5
	MaxWidth = 10

	// WidthEnv is the environment variable used to override the auto-detected
	// meter width.
	WidthEnv = "UKC_METER_WIDTH"
)

var (
	dots = []rune{'⣀', '⣄', '⣤', '⣦', '⣶', '⣷', '⣿'}

	emptyColor   = colors.AdaptiveColor{Light: colors.Slate300, Dark: colors.Slate700}
	safeColor    = colors.AdaptiveColor{Light: colors.Emerald600, Dark: colors.Emerald400}
	warningColor = colors.AdaptiveColor{Light: colors.Orange600, Dark: colors.Orange400}
	dangerColor  = colors.AdaptiveColor{Light: colors.Rose600, Dark: colors.Rose400}
)

// Width returns the width of the meter bar. It is read from the UKC_METER_WIDTH
// environment variable if set, otherwise it is derived from the detected
// terminal width. In both cases the result is clamped to [MinWidth, MaxWidth].
func Width() int {
	if v := os.Getenv(WidthEnv); v != "" {
		if width, err := strconv.Atoi(v); err == nil {
			return clampWidth(width)
		}
	}

	termWidth := guesstermwidth.GuessTermWidth(os.Stdout)

	return clampWidth(termWidth / 8)
}

// clampWidth clamps width to [MinWidth, MaxWidth].
func clampWidth(width int) int {
	if width < MinWidth {
		return MinWidth
	}
	if width > MaxWidth {
		return MaxWidth
	}
	return width
}

// Render renders a colored meter bar representing ratio at the given width,
// e.g. "⣿⣿⣿⣿⣿⣿⣿⣿⣀⣀". ratio is clamped to [0, 1]. The returned string ends
// with a foreground-color reset.
func Render(ratio float64, width int) string {
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}

	var color string
	switch {
	case ratio <= 0.6:
		color = ansi.NewStyle().ForegroundColor(safeColor).String()
	case ratio <= 0.8:
		color = ansi.NewStyle().ForegroundColor(warningColor).String()
	default:
		color = ansi.NewStyle().ForegroundColor(dangerColor).String()
	}

	empty := ansi.NewStyle().ForegroundColor(emptyColor).String()
	reset := ansi.NewStyle().ForegroundColor(nil).String()

	maxSteps := float64(width * (len(dots) - 1))
	steps := int(math.Round(ratio * maxSteps))

	fullCells := steps / (len(dots) - 1)
	rem := steps % (len(dots) - 1)

	var bar string
	if fullCells >= width {
		bar = color + strings.Repeat(string(dots[len(dots)-1]), width)
	} else {
		bar = color + strings.Repeat(string(dots[len(dots)-1]), fullCells)
		if rem > 0 {
			bar += string(dots[rem])
		}
		emptyCells := width - fullCells
		if rem > 0 {
			emptyCells--
		}
		if emptyCells > 0 {
			bar += empty + strings.Repeat(string(dots[0]), emptyCells)
		}
	}

	return bar + reset
}
