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
	shellHighlightCmdStyle     = lipgloss.NewStyle().Foreground(ShellAccentColor).Bold(true)
	shellHighlightStrStyle     = lipgloss.NewStyle().Foreground(colors.Success)
	shellHighlightVarStyle     = lipgloss.NewStyle().Foreground(colors.Warning)
	shellHighlightOptStyle     = lipgloss.NewStyle().Foreground(ShellDimColor)
	shellHighlightCommentStyle = lipgloss.NewStyle().Foreground(ShellHintColor).Italic(true)
	shellHighlightOpStyle      = lipgloss.NewStyle().Foreground(ShellAccentColor)
)

var (
	ShellAccentColor = compat.AdaptiveColor{Light: colors.Blue500, Dark: colors.Blue400}
	ShellDimColor    = compat.AdaptiveColor{Light: colors.Slate400, Dark: colors.Slate500}
	ShellHintColor   = compat.AdaptiveColor{Light: colors.Slate500, Dark: colors.Slate500}

	ShellTitleStyle  = lipgloss.NewStyle().Foreground(ShellAccentColor).Bold(true)
	ShellLabelStyle  = lipgloss.NewStyle().Foreground(ShellDimColor)
	ShellValueStyle  = lipgloss.NewStyle().Foreground(colors.Warning)
	ShellErrorStyle  = lipgloss.NewStyle().Foreground(colors.Error)
	ShellHintStyle   = lipgloss.NewStyle().Foreground(ShellHintColor)
	ShellKeyStyle    = lipgloss.NewStyle().Foreground(ShellDimColor).Faint(true)
	ShellPromptStyle = lipgloss.NewStyle().Foreground(colors.Success).Bold(true)
	ShellDirStyle    = lipgloss.NewStyle().Foreground(ShellAccentColor)
	ShellNoticeStyle = lipgloss.NewStyle().
				Foreground(colors.Info).
				Bold(true).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colors.Info).
				Padding(0, 1)
)
