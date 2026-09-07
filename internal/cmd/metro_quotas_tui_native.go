// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"unikraft.com/cli/internal/config"
	uitui "unikraft.com/cli/internal/tui/uitui"
	xio "unikraft.com/cli/internal/x/io"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/colors"
)

// runQuotasTUI shows quota usage in an interactive full-screen view.
func runQuotasTUI(ctx context.Context, stdio config.Stdio, cmd *MetroQuotasCmd, watch *time.Duration) error {
	m := newQuotasModel(ctx, cmd.Metro, watch)
	p := tea.NewProgram(m, tea.WithInput(stdio.Stdin), tea.WithOutput(xio.Unwrap(stdio.Stdout)))
	_, err := p.Run()
	return err
}

type quotasModel struct {
	ctx           context.Context
	metros        []string
	watchInterval *time.Duration
	lastRefresh   time.Time
	tabs          []string
	activeTab     int
	termWidth     int
	termHgt       int
	data          map[string]*platform.Quotas
	errors        map[string]error
	userName      string
	loading       bool
	err           error
}

type (
	quotasLoadedMsg = multiMetroResult
	quotasTickMsg   struct{}
	quotasStatusMsg struct{}
)

func computeQuotaBarWidth(termWidth int) int {
	if termWidth <= 0 {
		return defaultQuotaBarWidth
	}
	const reserved = 42
	w := termWidth - reserved
	return max(minQuotaBarWidth, min(w, defaultQuotaBarWidth))
}

func newQuotasModel(ctx context.Context, metros []string, watchInterval *time.Duration) quotasModel {
	var tabs []string
	if len(metros) == 1 {
		tabs = []string{metros[0]}
	}
	return quotasModel{
		ctx:           ctx,
		metros:        metros,
		watchInterval: watchInterval,
		tabs:          tabs,
		data:          make(map[string]*platform.Quotas),
		errors:        make(map[string]error),
		loading:       true,
	}
}

func (m quotasModel) Init() tea.Cmd {
	if m.watchInterval != nil {
		return tea.Batch(m.fetchCmd(), m.watchTickCmd(), m.watchStatusTickCmd())
	}
	return m.fetchCmd()
}

func (m quotasModel) fetchCmd() tea.Cmd {
	ctx := m.ctx
	metros := slices.Clone(m.metros)
	return func() tea.Msg {
		return fetchAllMetros(ctx, metros)
	}
}

func (m quotasModel) watchTickCmd() tea.Cmd {
	if m.watchInterval == nil {
		return nil
	}
	return tea.Tick(*m.watchInterval, func(time.Time) tea.Msg {
		return quotasTickMsg{}
	})
}

func (m quotasModel) watchStatusTickCmd() tea.Cmd {
	if m.watchInterval == nil {
		return nil
	}
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return quotasStatusMsg{}
	})
}

func (m quotasModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case quotasLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.lastRefresh = time.Now()
		m.userName = msg.userName
		m.data = msg.data
		m.errors = msg.errors
		m.tabs = msg.tabs

		if len(m.tabs) == 0 && len(m.metros) == 1 {
			m.tabs = []string{m.metros[0]}
		}
		if m.activeTab >= len(m.tabs) {
			m.activeTab = 0
		}
		return m, nil

	case quotasTickMsg:
		m.loading = true
		return m, tea.Batch(m.fetchCmd(), m.watchTickCmd())

	case quotasStatusMsg:
		return m, m.watchStatusTickCmd()

	case tea.KeyPressMsg:
		switch msg.Keystroke() {
		case "ctrl+c", "q", "escape":
			return m, tea.Quit
		case "tab", "right", "l":
			if len(m.tabs) > 1 {
				m.activeTab = (m.activeTab + 1) % len(m.tabs)
			}
		case "shift+tab", "left", "h":
			if len(m.tabs) > 1 {
				m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			}
		}

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHgt = msg.Height
	}
	return m, nil
}

func (m quotasModel) View() tea.View {
	var view strings.Builder
	if m.err != nil {
		return tea.NewView(fmt.Sprintf("  Error: %v\n", m.err))
	}

	// Render tabs
	view.WriteString("  ")
	for i, tab := range m.tabs {
		label := tab
		if _, ok := m.errors[tab]; ok {
			label = tab + "!"
		}
		if i == m.activeTab {
			style := lipgloss.NewStyle().Foreground(colors.Slate50).Background(colors.Primary).Padding(0, 1)
			if _, ok := m.errors[tab]; ok {
				style = style.Foreground(colors.Warning)
			}
			view.WriteString(style.Render(label))
		} else {
			style := lipgloss.NewStyle().Faint(true).Padding(0, 1)
			if _, ok := m.errors[tab]; ok {
				style = style.Foreground(colors.Warning)
			}
			view.WriteString(style.Render(label))
		}
		if i < len(m.tabs)-1 {
			view.WriteString(" ")
		}
	}
	view.WriteString("\n\n")

	// Render quota view
	if m.loading && len(m.data) == 0 {
		view.WriteString("  Loading quotas...\n")
		return tea.NewView(view.String())
	}

	var q *platform.Quotas
	if m.activeTab < len(m.tabs) {
		tab := m.tabs[m.activeTab]
		q = m.data[tab]
	}

	if q == nil {
		if err := m.errors[m.tabs[m.activeTab]]; err != nil {
			view.WriteString("  ")
			view.WriteString(uitui.ErrorStyle.Render(err.Error()))
			view.WriteString("\n")
		} else {
			view.WriteString("  No quota data available.\n")
		}
	} else {
		view.WriteString(renderQuotaView(q, m.userName, computeQuotaBarWidth(m.termWidth)))
	}

	view.WriteString("\n")
	view.WriteString(lipgloss.NewStyle().Italic(true).Faint(true).Render("  Tab/←→: switch metro  q: quit"))

	if m.watchInterval != nil && !m.lastRefresh.IsZero() {
		elapsed := time.Since(m.lastRefresh).Seconds()
		status := fmt.Sprintf("  last refreshed %.1fs ago", elapsed)
		status = lipgloss.NewStyle().Italic(true).Faint(true).Render(status)
		view.WriteString(status)
	}

	quotaView := view.String()

	viewW := max(computeQuotaBarWidth(m.termWidth), maxLineWidth(quotaView))
	viewH := lineCount(quotaView)
	if m.termWidth > 0 && m.termHgt > 0 && (m.termWidth < viewW || m.termHgt < viewH) {
		var b strings.Builder
		b.WriteString("  Terminal too small for quota view.\n")
		b.WriteString("  Increase terminal size.\n\n")
		fmt.Fprintf(&b, "  Current: width=%d height=%d\n", m.termWidth, m.termHgt)
		fmt.Fprintf(&b, "  Needed:  width=%d height=%d\n", viewW, viewH)
		return tea.NewView(b.String())
	}

	return tea.NewView(quotaView)
}

func lineCount(s string) int {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func maxLineWidth(s string) int {
	maxWidth := 0
	for line := range strings.SplitSeq(strings.TrimSuffix(s, "\n"), "\n") {
		w := lipgloss.Width(line)
		if w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}
