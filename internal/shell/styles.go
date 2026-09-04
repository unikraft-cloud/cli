// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"charm.land/lipgloss/v2"
	"unikraft.com/x/colors"
)

var (
	promptStyle       = lipgloss.NewStyle().Foreground(colors.Primary).Bold(true)
	promptDirStyle    = lipgloss.NewStyle().Foreground(colors.Slate500)
	continuationStyle = lipgloss.NewStyle().Foreground(colors.Slate500)
	errorStyle        = lipgloss.NewStyle().Foreground(colors.Error)
	hintStyle         = lipgloss.NewStyle().Foreground(colors.Slate500)

	bannerStyle = lipgloss.NewStyle().
			Foreground(colors.Warning).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colors.Warning).
			Padding(0, 1)

	highlightStringStyle  = lipgloss.NewStyle().Foreground(colors.Emerald400)
	highlightBuiltinStyle = lipgloss.NewStyle().Foreground(colors.Primary).Bold(true)
	highlightSpecialStyle = lipgloss.NewStyle().Foreground(colors.Orange400)
)
