// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tui

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/MakeNowJust/heredoc"
	"github.com/charmbracelet/x/ansi"

	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/tui/monkey"
	"unikraft.com/cli/internal/tui/uitui"
	"unikraft.com/x/colors"
)

const (
	homeWebsiteURL = "https://unikraft.com"
	homeGitHubURL  = "https://github.com/unikraft-cloud/cli"
)

var homeArt = heredoc.Docf(`
	  __ _____  (_) /__ _______ _/ _/ /_
	 / // / _ \/ /  '_// __/ _ %[1]s/ _/ __/
	\_,_/_//_/_/_/\_\/_/  \_,_/_/ \__/`, "`")

type homePanel struct {
	ctx      context.Context
	registry *model.Registry

	entries []*model.ModelDescriptor
	cursor  int

	monkey monkey.Model

	width   int
	height  int
	focused bool
}

func NewHomePanel(ctx context.Context, registry *model.Registry) tea.Model {
	panel := &homePanel{
		ctx:      ctx,
		registry: registry,
		monkey:   monkey.New(),
	}
	panel.syncModels()
	return panel
}

func (p *homePanel) Init() tea.Cmd {
	return p.monkey.Init()
}

func (p *homePanel) Breadcrumb() string {
	return "Home"
}

func (p *homePanel) Actions() []uitui.Action {
	if p.currentDescriptor() == nil {
		return nil
	}
	return []uitui.Action{{
		Label: "open",
		Keys:  []string{"enter"},
		Value: actionOpen{},
	}}
}

func (p *homePanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	monkeyModel, monkeyCmd := p.monkey.Update(msg)
	p.monkey = monkeyModel
	if monkeyCmd != nil {
		cmds = append(cmds, monkeyCmd)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, tea.Batch(cmds...)
	case uitui.PanelFocusMsg:
		p.focused = msg.Focused
		return p, tea.Batch(cmds...)
	case actionOpen:
		return p, p.openSelected()
	case tea.KeyMsg:
		if !p.focused {
			return p, tea.Batch(cmds...)
		}
		switch msg.String() {
		case "up", "k":
			p.cursor = max(0, p.cursor-1)
		case "down", "j":
			p.cursor = min(max(0, len(p.entries)-1), p.cursor+1)
		}
		return p, tea.Batch(cmds...)
	}

	return p, tea.Batch(cmds...)
}

func (p *homePanel) View() tea.View {
	contentWidth := p.width
	contentHeight := p.height
	if contentHeight == 0 || contentWidth == 0 {
		return tea.NewView("")
	}

	artBlock := lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, homeArt)
	monkeyBlock := lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, p.monkey.View())

	bodyParts := []string{artBlock, monkeyBlock}
	if linksLine := p.linksLine(); linksLine != "" {
		bodyParts = append(bodyParts, "", lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, strings.TrimRight(linksLine, " ")))
	}
	if len(p.entries) > 0 {
		listView := p.renderEntries()
		listBlock := lipgloss.PlaceHorizontal(contentWidth, lipgloss.Center, listView)
		bodyParts = append(bodyParts, "", listBlock)
	}

	body := strings.Join(bodyParts, "\n")
	body = lipgloss.NewStyle().MaxHeight(contentHeight).Render(body)
	body = lipgloss.PlaceVertical(contentHeight, lipgloss.Center, body)

	return tea.NewView(body)
}

func (p *homePanel) syncModels() {
	if p.registry == nil {
		p.entries = nil
		p.cursor = 0
		return
	}

	entries := slices.Clone(p.registry.Entries())
	slices.SortFunc(entries, func(a, b *model.ModelDescriptor) int {
		return strings.Compare(a.Name, b.Name)
	})
	p.entries = entries
	if len(p.entries) == 0 {
		p.cursor = 0
	} else {
		p.cursor = min(max(p.cursor, 0), len(p.entries)-1)
	}
}

func (p *homePanel) openSelected() tea.Cmd {
	entry := p.currentDescriptor()
	if entry == nil {
		return nil
	}
	panel := NewListPanel(p.ctx, p.registry, entry)
	return func() tea.Msg {
		return uitui.OpenPanelMsg{Panel: panel, Collapse: true}
	}
}

func (p *homePanel) currentDescriptor() *model.ModelDescriptor {
	if p.cursor < 0 || p.cursor >= len(p.entries) {
		return nil
	}
	return p.entries[p.cursor]
}

func (p *homePanel) linksLine() string {
	linkStyle := lipgloss.NewStyle().Foreground(colors.Primary)
	return strings.Join([]string{
		linkStyle.Render(hyperlink("Website", homeWebsiteURL)),
		linkStyle.Render(hyperlink("GitHub", homeGitHubURL)),
	}, " • ")
}

func (p *homePanel) renderEntries() string {
	if len(p.entries) == 0 {
		return ""
	}

	maxWidth := 0
	for _, entry := range p.entries {
		maxWidth = max(maxWidth, ansi.StringWidth(entry.Names))
	}

	normalStyle := lipgloss.NewStyle().Width(maxWidth).Align(lipgloss.Left)
	selectedStyle := normalStyle.Bold(true).Foreground(colors.Primary)

	lines := make([]string, 0, len(p.entries))
	for i, entry := range p.entries {
		style := normalStyle
		if i == p.cursor {
			style = selectedStyle
		}
		lines = append(lines, style.Render(entry.Names))
	}
	return strings.Join(lines, "\n")
}
