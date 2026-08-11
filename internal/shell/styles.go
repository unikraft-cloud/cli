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
	shellHighlightCmdStyle = lipgloss.NewStyle().Foreground(shellAccentColor).Bold(true)
	// Builtins take the prompt's colour rather than a colour of their own:
	// green in the line means the CLI answers it, blue means the instance
	// does.
	shellHighlightBuiltinStyle = lipgloss.NewStyle().Foreground(colors.Success).Bold(true)
	shellHighlightStrStyle     = lipgloss.NewStyle().Foreground(colors.Success)
	shellHighlightVarStyle     = lipgloss.NewStyle().Foreground(colors.Warning)
	shellHighlightOptStyle     = lipgloss.NewStyle().Foreground(shellDimColor)
	shellHighlightCommentStyle = lipgloss.NewStyle().Foreground(shellHintColor).Italic(true)
	shellHighlightOpStyle      = lipgloss.NewStyle().Foreground(shellAccentColor)
)

var (
	shellAccentColor = compat.AdaptiveColor{Light: colors.Blue500, Dark: colors.Blue400}
	shellDimColor    = compat.AdaptiveColor{Light: colors.Slate400, Dark: colors.Slate500}
	shellHintColor   = compat.AdaptiveColor{Light: colors.Slate500, Dark: colors.Slate500}

	ShellTitleStyle  = lipgloss.NewStyle().Foreground(shellAccentColor).Bold(true)
	ShellLabelStyle  = lipgloss.NewStyle().Foreground(shellDimColor)
	ShellValueStyle  = lipgloss.NewStyle().Foreground(colors.Warning)
	ShellErrorStyle  = lipgloss.NewStyle().Foreground(colors.Error)
	ShellHintStyle   = lipgloss.NewStyle().Foreground(shellHintColor)
	ShellKeyStyle    = lipgloss.NewStyle().Foreground(shellDimColor).Faint(true)
	shellPromptStyle = lipgloss.NewStyle().Foreground(colors.Success).Bold(true)
	ShellDirStyle    = lipgloss.NewStyle().Foreground(shellAccentColor)
	ShellNoticeStyle = lipgloss.NewStyle().
				Foreground(colors.Info).
				Bold(true).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colors.Info).
				Padding(0, 1)
)
