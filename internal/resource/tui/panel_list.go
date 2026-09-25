// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"unikraft.com/cli/internal/resource"
	resourcecmd "unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/tui/styles"
	"unikraft.com/cli/internal/tui/uitui"
	xslices "unikraft.com/cli/internal/x/slices"
)

type listPanel struct {
	ctx      context.Context
	registry *resource.Registry
	desc     *resource.ResourceDescriptor

	table   table.Model
	headers []string
	rowKeys []string

	err     error
	loading bool
	width   int
	height  int
	focused bool
}

type listDataMsg struct {
	resources []resource.Resource
	err       error
}

func NewListPanel(ctx context.Context, registry *resource.Registry, desc *resource.ResourceDescriptor) *listPanel {
	return &listPanel{
		ctx:      ctx,
		registry: registry,
		desc:     desc,
		table:    table.New(table.WithFocused(true), table.WithStyles(uitui.DefaultTableStyles)),
	}
}

func (p *listPanel) Init() tea.Cmd {
	return nil
}

func (p *listPanel) Title() string {
	return ""
}

func (p *listPanel) Breadcrumb() string {
	if p.desc == nil {
		return ""
	}
	return strings.TrimSpace(p.desc.Names)
}

func (p *listPanel) Actions() []uitui.Action {
	actions := []uitui.Action{
		{
			Label: "refresh",
			Keys:  []string{"r"},
			Value: actionRefresh{},
		},
	}
	if p.desc != nil && p.desc.Get != nil && len(p.rowKeys) > 0 {
		actions = append([]uitui.Action{
			{
				Label: "open",
				Keys:  []string{"enter"},
				Value: actionOpen{},
			},
		}, actions...)
	}
	return actions
}

func (p *listPanel) Refresh() tea.Cmd {
	if p.loading || p.desc == nil {
		return nil
	}
	listable := p.desc.List
	if listable == nil {
		return nil
	}
	p.loading = true
	return func() tea.Msg {
		resources, err := listable.List(p.ctx)
		return listDataMsg{resources: resources, err: err}
	}
}

func (p *listPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.layout()
		return p, nil
	case uitui.PanelFocusMsg:
		p.focused = msg.Focused
		p.layout()
		return p, nil
	case listDataMsg:
		p.loading = false
		if msg.err != nil {
			p.err = msg.err
			return p, nil
		}
		p.err = nil
		p.applyResources(msg.resources)
		p.layout()
		return p, nil
	case actionOpen:
		return p, p.openSelected()
	case actionRefresh:
		return p, p.Refresh()
	case tea.KeyMsg:
		updated, cmd := p.table.Update(msg)
		p.table = updated
		return p, cmd
	}

	return p, nil
}

func (p *listPanel) View() tea.View {
	if p.err != nil {
		return tea.NewView(styles.Error.Render(p.err.Error()))
	}
	if p.loading && len(p.rowKeys) == 0 {
		return tea.NewView(styles.Hint.Render("Loading..."))
	}
	if len(p.rowKeys) == 0 {
		return tea.NewView(styles.Hint.Render("No results"))
	}

	return tea.NewView(p.table.View())
}

func (p *listPanel) applyResources(resources []resource.Resource) {
	if p.desc == nil {
		p.headers = nil
		p.rowKeys = nil
		p.table.SetRows(nil)
		return
	}
	fields, err := p.desc.List.Fields(p.ctx)
	if err != nil {
		p.err = err
		p.table.SetRows(nil)
		return
	}
	fields, err = resourcecmd.SelectFields(fields, true, resource.FieldVerbosityShort, nil)
	if err != nil {
		p.err = err
		p.table.SetRows(nil)
		return
	}
	for i := range fields {
		fields[i] = resource.PruneFields(fields[i])
	}

	paths, headers := xslices.Collect2(resource.IterFields(fields))
	colPaths := make([]resource.FieldPath, 0, len(headers))
	colHeaders := make([]string, 0, len(headers))
	for i, header := range headers {
		if header.HasChildren() && header.Value == nil {
			continue
		}
		path := paths[i]
		colPaths = append(colPaths, path)
		colHeaders = append(colHeaders, strings.ToUpper(path.Leaf()))
	}

	rows := make([]table.Row, 0, len(resources))
	keys := make([]string, 0, len(resources))
	for _, res := range resources {
		cells := make([]string, 0, len(colPaths))
		for _, path := range colPaths {
			cells = append(cells, p.renderField(res, path))
		}
		rows = append(rows, table.Row(cells))
		keys = append(keys, res.Key().String())
	}

	p.headers = colHeaders
	p.rowKeys = keys
	p.table.SetColumns(buildColumns(p.headers, rows, 0))
	p.table.SetRows(rows)
	if len(rows) == 0 {
		p.table.SetCursor(0)
		return
	}
	if p.table.Cursor() >= len(rows) {
		p.table.SetCursor(len(rows) - 1)
	}
}

func (p *listPanel) openSelected() tea.Cmd {
	if p.desc == nil || p.desc.Get == nil || len(p.rowKeys) == 0 {
		return nil
	}
	idx := p.table.Cursor()
	if idx < 0 || idx >= len(p.rowKeys) {
		return nil
	}
	panel := NewDetailPanel(p.ctx, p.registry, p.desc, p.rowKeys[idx])
	return func() tea.Msg {
		return uitui.OpenPanelMsg{Panel: panel, Collapse: false}
	}
}

func (p *listPanel) configureTable(width, height int, focused bool) {
	if width <= 0 || height <= 0 {
		return
	}
	if focused {
		p.table.Focus()
	} else {
		p.table.Blur()
	}
	if len(p.headers) > 0 {
		p.table.SetColumns(buildColumns(p.headers, p.table.Rows(), width))
	}
	p.table.SetHeight(height)
	p.table.SetWidth(width)
}

func (p *listPanel) layout() {
	p.configureTable(p.width, p.height, p.focused)
}

func (p *listPanel) renderField(res resource.Resource, path resource.FieldPath) string {
	fields, err := res.Fields(p.ctx)
	if err != nil {
		return ""
	}
	fields = resource.GetFieldByPath(fields, path)
	for i := range fields {
		fields[i] = resource.PruneFields(fields[i])
	}
	values := make([]string, 0)
	for _, field := range resource.IterFields(fields) {
		if field.Value == nil {
			continue
		}
		value, err := field.Render(value.RenderOpts{Short: true})
		if err != nil {
			continue
		}
		value = hyperlink(value, field.Hyperlink)
		values = append(values, value)
	}
	return strings.Join(values, ", ")
}
