// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package uitui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"unikraft.com/x/colors"
)

// Exported styles for use outside package
var (
	DefaultTableStyles = table.Styles{
		Header:   table.DefaultStyles().Header,
		Cell:     table.DefaultStyles().Cell,
		Selected: tableSelectedStyle,
	}
)

// Private color variables and styles used internally
var (
	borderColor        = compat.AdaptiveColor{Light: colors.Slate300, Dark: colors.Slate700}
	focusedBorderColor = compat.AdaptiveColor{Light: colors.Blue400, Dark: colors.Slate400}
	menuColor          = compat.AdaptiveColor{Light: colors.Slate600, Dark: colors.Slate400}
	hintColor          = compat.AdaptiveColor{Light: colors.Slate500, Dark: colors.Slate500}
	selectedRowBgColor = compat.AdaptiveColor{Light: colors.Slate100, Dark: colors.Slate800}

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(0, 1)

	panelFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(focusedBorderColor).
				Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Foreground(colors.Primary).
			Bold(true)

	breadcrumbStyle          = lipgloss.NewStyle().Foreground(hintColor)
	breadcrumbCurrentStyle   = lipgloss.NewStyle().Foreground(colors.Primary).Bold(true)
	breadcrumbSeparatorStyle = lipgloss.NewStyle().Foreground(hintColor)
	breadcrumbContainerStyle = lipgloss.NewStyle().PaddingLeft(1)

	helpContainerStyle = lipgloss.NewStyle().PaddingLeft(1)

	tableSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Background(selectedRowBgColor)

	helpStyles = help.Styles{
		Ellipsis:       lipgloss.NewStyle().Foreground(hintColor),
		ShortKey:       lipgloss.NewStyle().Foreground(menuColor).Bold(true),
		ShortDesc:      lipgloss.NewStyle().Foreground(hintColor),
		ShortSeparator: lipgloss.NewStyle().Foreground(hintColor),
		FullKey:        lipgloss.NewStyle().Foreground(menuColor).Bold(true),
		FullDesc:       lipgloss.NewStyle().Foreground(hintColor),
		FullSeparator:  lipgloss.NewStyle().Foreground(hintColor),
	}
)
