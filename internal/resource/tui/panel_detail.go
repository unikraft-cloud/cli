// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/ansi"
	"unikraft.com/x/colors"

	"unikraft.com/cli/internal/resource"
	resourcecmd "unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/tui/styles"
	"unikraft.com/cli/internal/tui/uitui"
)

type detailPanel struct {
	ctx      context.Context
	registry *resource.Registry
	desc     *resource.ResourceDescriptor
	key      string

	table    table.Model
	rowLinks []resource.Link

	err     error
	loading bool
	width   int
	height  int
	focused bool
}

type kvDataMsg struct {
	resource resource.Resource
	err      error
}

func NewDetailPanel(ctx context.Context, registry *resource.Registry, desc *resource.ResourceDescriptor, key string) *detailPanel {
	return &detailPanel{
		ctx:      ctx,
		registry: registry,
		desc:     desc,
		key:      key,
		table:    table.New(table.WithFocused(true), table.WithStyles(uitui.DefaultTableStyles)),
	}
}

func (p *detailPanel) Init() tea.Cmd {
	return nil
}

func (p *detailPanel) Title() string {
	return ""
}

func (p *detailPanel) Breadcrumb() string {
	if p.desc == nil {
		return ""
	}
	base := p.desc.Name
	if p.key == "" {
		return strings.TrimSpace(base)
	}
	return strings.TrimSpace(base + " " + p.key)
}

func (p *detailPanel) Actions() []uitui.Action {
	actions := []uitui.Action{
		{
			Label: "refresh",
			Keys:  []string{"r"},
			Value: actionRefresh{},
		},
	}
	if p.currentLink() != nil {
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

func (p *detailPanel) Refresh() tea.Cmd {
	if p.loading || p.desc == nil || p.desc.Get == nil || p.key == "" {
		return nil
	}
	gettable := p.desc.Get
	p.loading = true
	return func() tea.Msg {
		resources, err := gettable.Get(p.ctx, []string{p.key})
		if err != nil {
			return kvDataMsg{err: err}
		}
		if len(resources) == 0 {
			return kvDataMsg{err: fmt.Errorf("resource not found: %s", p.key)}
		}
		return kvDataMsg{resource: resources[0]}
	}
}

func (p *detailPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case kvDataMsg:
		p.loading = false
		if msg.err != nil {
			p.err = msg.err
			return p, nil
		}
		p.err = nil
		p.renderResource(msg.resource)
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

func (p *detailPanel) View() tea.View {
	if p.err != nil {
		return tea.NewView(styles.Error.Render(p.err.Error()))
	}
	if p.loading && len(p.rowLinks) == 0 {
		return tea.NewView(styles.Hint.Render("Loading..."))
	}
	if len(p.rowLinks) == 0 {
		return tea.NewView(styles.Hint.Render("No fields"))
	}

	return tea.NewView(p.table.View())
}

func (p *detailPanel) renderResource(res resource.Resource) {
	fields, err := res.Fields(p.ctx)
	if err != nil {
		p.err = err
		p.table.SetRows(nil)
		p.rowLinks = nil
		return
	}
	fields, err = resourcecmd.SelectFields(fields, false, resource.FieldVerbosityLong, nil)
	if err != nil {
		p.err = err
		p.table.SetRows(nil)
		p.rowLinks = nil
		return
	}
	fields = resource.DedupeFields(fields)

	rows := make([]table.Row, 0)
	links := make([]resource.Link, 0)
	if err := appendKVRows(&rows, &links, nil, fields, 0); err != nil {
		p.err = err
		p.table.SetRows(nil)
		p.rowLinks = nil
		return
	}

	p.rowLinks = links
	p.table.SetColumns(buildColumns([]string{"Field", "Value"}, rows, 0))
	p.table.SetRows(rows)
	if len(rows) == 0 {
		p.table.SetCursor(0)
		return
	}
	if p.table.Cursor() >= len(rows) {
		p.table.SetCursor(len(rows) - 1)
	}
}

func (p *detailPanel) openSelected() tea.Cmd {
	link := p.currentLink()
	if link == nil {
		return nil
	}
	linkType, linkKey, _ := link.Link()
	if linkType == "" || linkKey == nil {
		return nil
	}
	key := linkKey.String()
	if key == "" {
		return nil
	}
	if p.registry == nil {
		p.err = fmt.Errorf("unknown resource type: %s", linkType)
		return nil
	}
	desc, ok := p.registry.Resolve(linkType)
	if !ok {
		p.err = fmt.Errorf("unknown resource type: %s", linkType)
		return nil
	}
	panel := NewDetailPanel(p.ctx, p.registry, desc, key)
	return func() tea.Msg {
		return uitui.OpenPanelMsg{Panel: panel, Collapse: false}
	}
}

func (p *detailPanel) currentLink() resource.Link {
	idx := p.table.Cursor()
	if idx < 0 || idx >= len(p.rowLinks) {
		return nil
	}
	return p.rowLinks[idx]
}

func (p *detailPanel) configureTable(width, height int, focused bool) {
	if width <= 0 || height <= 0 {
		return
	}
	if focused {
		p.table.Focus()
	} else {
		p.table.Blur()
	}
	p.table.SetColumns(buildColumns([]string{"Field", "Value"}, p.table.Rows(), width))
	p.table.SetHeight(height)
	p.table.SetWidth(width)
}

func (p *detailPanel) layout() {
	p.configureTable(p.width, p.height, p.focused)
}

func appendKVRows(rows *[]table.Row, links *[]resource.Link, parent *resource.Field, fields []resource.Field, indent int) error {
	linkColor := compat.AdaptiveColor{Light: colors.Slate600, Dark: colors.Slate400}
	linkSeq := ansi.NewStyle(ansi.AttrItalic, ansi.AttrUnderline).ForegroundColor(compat.Profile.Convert(linkColor)).String()
	linkReset := ansi.NewStyle(ansi.AttrNoItalic, ansi.AttrNoUnderline).ForegroundColor(nil).String()

	for _, field := range fields {
		prefix := strings.Repeat("  ", indent)
		link := firstLink(field)

		if parent != nil && parent.Elem != nil {
			// Array element - always show subfields in TUI, never use Value
			if len(field.Subfields) > 0 {
				// Has subfields - render them with `-` prefix on first one
				usedElementLink := false
				for j, subfield := range field.Subfields {
					var key string
					subLink := firstLink(subfield)
					if subLink == nil && link != nil && !usedElementLink {
						subLink = link
						usedElementLink = true
					}
					if j == 0 {
						// First subfield gets the `-` prefix
						key = prefix + "- " + subfield.Name
					} else {
						// Subsequent subfields: indent to align with first
						key = prefix + "  " + subfield.Name
					}
					if subLink != nil {
						key = linkSeq + key + linkReset
					}

					// Prefer subfields over value for detail view
					if len(subfield.Subfields) > 0 {
						*rows = append(*rows, table.Row{key, ""})
						*links = append(*links, subLink)
						if err := appendKVRows(rows, links, &subfield, subfield.Subfields, indent+1); err != nil {
							return err
						}
					} else if subfield.Value != nil {
						value, err := subfield.Render(value.RenderOpts{})
						if err != nil {
							return err
						}
						value = hyperlink(value, subfield.Hyperlink)
						*rows = append(*rows, table.Row{key, value})
						*links = append(*links, subLink)
					} else {
						*rows = append(*rows, table.Row{key, ""})
						*links = append(*links, subLink)
					}
				}
			} else {
				// No subfields - use value if present
				key := prefix + "-"
				if link != nil {
					key = linkSeq + key + linkReset
				}
				if field.Value != nil {
					value, err := field.Render(value.RenderOpts{})
					if err != nil {
						return err
					}
					value = hyperlink(value, field.Hyperlink)
					*rows = append(*rows, table.Row{key, value})
					*links = append(*links, link)
				} else {
					*rows = append(*rows, table.Row{key, ""})
					*links = append(*links, link)
				}
			}
			continue
		}

		key := prefix + field.Name
		if link != nil {
			key = linkSeq + key + linkReset
		}
		if field.Value == nil {
			*rows = append(*rows, table.Row{key, ""})
			*links = append(*links, link)
			if err := appendKVRows(rows, links, &field, field.Subfields, indent+1); err != nil {
				return err
			}
			continue
		}
		value, err := field.Render(value.RenderOpts{})
		if err != nil {
			return err
		}
		value = hyperlink(value, field.Hyperlink)
		*rows = append(*rows, table.Row{key, value})
		*links = append(*links, link)
	}
	return nil
}

func firstLink(field resource.Field) resource.Link {
	if len(field.Links) == 0 {
		return nil
	}
	return field.Links[0]
}

func (p *detailPanel) Subpanels() []tea.Model {
	if p.desc == nil || p.key == "" {
		return nil
	}
	if p.desc.Get == nil {
		return nil
	}
	provider, ok := p.desc.Get.(Subpanels)
	if !ok {
		return nil
	}
	return provider.Subpanels(p.ctx, p.key)
}
