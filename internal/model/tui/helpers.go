// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tui

import (
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"unikraft.com/cli/internal/tableutil"
)

func buildColumns(headers []string, rows []table.Row, width int) []table.Column {
	cols := make([]table.Column, len(headers))
	if len(headers) == 0 {
		return cols
	}

	contentWidths := make([]int, len(headers))
	for i, header := range headers {
		contentWidths[i] = ansi.StringWidth(header)
	}
	for _, row := range rows {
		for i := 0; i < len(headers) && i < len(row); i++ {
			contentWidths[i] = max(contentWidths[i], ansi.StringWidth(row[i]))
		}
	}

	colPadding := make([]int, len(headers))

	if width > 0 {
		cellPadding := table.DefaultStyles().Cell.GetHorizontalFrameSize()
		available := width - cellPadding*len(headers)
		if available <= 0 {
			for i := range contentWidths {
				contentWidths[i] = 0
			}
			for i := range colPadding {
				colPadding[i] = 0
			}
		} else {
			total := sumWidths(contentWidths, colPadding)
			if total > available {
				over := total - available
				tableutil.ReduceColumns(contentWidths, over)
			}
		}
	}

	for i, header := range headers {
		cols[i] = table.Column{Title: header, Width: max(0, contentWidths[i]+colPadding[i])}
	}
	return cols
}

func sumWidths(content []int, padding []int) int {
	widthSum := 0
	for _, p := range padding {
		widthSum += p
	}
	for _, w := range content {
		widthSum += w
	}
	return widthSum
}

func hyperlink(s, url string) string {
	if url == "" {
		return s
	}
	if compat.Profile == colorprofile.NoTTY {
		return s
	}
	underlineSeq := ansi.NewStyle(ansi.AttrUnderline).String()
	underlineReset := ansi.NewStyle(ansi.AttrNoUnderline).String()
	return ansi.SetHyperlink(url) + underlineSeq + s + underlineReset + ansi.ResetHyperlink()
}
