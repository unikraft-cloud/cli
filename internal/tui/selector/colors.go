// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package selector

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"unikraft.com/x/colors"
)

var (
	dimmestColor = compat.AdaptiveColor{Light: colors.Slate600, Dark: colors.Slate600}
	dimmerColor  = compat.AdaptiveColor{Light: colors.Slate700, Dark: colors.Slate500}
	dimColor     = compat.AdaptiveColor{Light: colors.Slate400, Dark: colors.Slate400}

	dimmestStyle = lipgloss.NewStyle().
			Foreground(dimmestColor).
			Render
	dimStyle = lipgloss.NewStyle().
			Foreground(dimColor).
			Render

	pipeLineColor = compat.AdaptiveColor{Light: colors.Slate400, Dark: colors.Slate400}
	pipeLineStyle = lipgloss.NewStyle().
			Background(pipeLineColor).
			Foreground(pipeLineColor).
			Render
	cursorLineColor = compat.AdaptiveColor{Light: colors.Slate300, Dark: colors.Slate500}
	cursorLineStyle = lipgloss.NewStyle().
			Background(cursorLineColor).
			Foreground(cursorLineColor).
			Render
	selectLineColor = colors.Primary
	selectLineStyle = lipgloss.NewStyle().
			Background(selectLineColor).
			Foreground(selectLineColor).
			Render
)
