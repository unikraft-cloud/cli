// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"

	"unikraft.com/x/colors"
)

var (
	highlightCmdStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	// Builtins take the prompt's colour rather than one of their own: green in
	// the line means the CLI answers it, blue means the instance does.
	highlightBuiltinStyle = lipgloss.NewStyle().Foreground(colors.Success).Bold(true)
	highlightStrStyle     = lipgloss.NewStyle().Foreground(colors.Success)
	highlightVarStyle     = lipgloss.NewStyle().Foreground(colors.Warning)
	highlightOptStyle     = lipgloss.NewStyle().Foreground(dimColor)
	highlightCommentStyle = lipgloss.NewStyle().Foreground(hintColor).Italic(true)
	highlightOpStyle      = lipgloss.NewStyle().Foreground(accentColor)
)

var (
	accentColor = compat.AdaptiveColor{Light: colors.Blue500, Dark: colors.Blue400}
	dimColor    = compat.AdaptiveColor{Light: colors.Slate400, Dark: colors.Slate500}
	hintColor   = compat.AdaptiveColor{Light: colors.Slate500, Dark: colors.Slate500}

	titleStyle  = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	labelStyle  = lipgloss.NewStyle().Foreground(dimColor)
	valueStyle  = lipgloss.NewStyle().Foreground(colors.Warning)
	errorStyle  = lipgloss.NewStyle().Foreground(colors.Error)
	hintStyle   = lipgloss.NewStyle().Foreground(hintColor)
	keyStyle    = lipgloss.NewStyle().Foreground(dimColor).Faint(true)
	promptStyle = lipgloss.NewStyle().Foreground(colors.Success).Bold(true)
	dirStyle    = lipgloss.NewStyle().Foreground(accentColor)
	noticeStyle = lipgloss.NewStyle().
			Foreground(colors.Info).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colors.Info).
			Padding(0, 1)
)
