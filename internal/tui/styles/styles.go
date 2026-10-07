// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package styles holds lipgloss styles that do not use Bubble Tea.
package styles

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"

	"unikraft.com/x/colors"
)

var (
	Error = lipgloss.NewStyle().Foreground(colors.Error)
	Hint  = lipgloss.NewStyle().Foreground(compat.AdaptiveColor{Light: colors.Slate500, Dark: colors.Slate500})
)
