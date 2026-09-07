// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package selector

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	jujuerrors "github.com/juju/errors"

	"unikraft.com/x/colors"
)

var ErrNoOptionSelected = jujuerrors.New("no option selected")

// Single is a utility method used in a CLI context to prompt the
// user to pick exactly one option from a slice of options based on the
// generic type.
func Single[T ~string](question string, options ...T) (T, error) {
	return singleSelect(question, "", options)
}

// SingleWithDefault behaves like [Single] but pre-positions the cursor on
// the option whose representation matches defaultValue.
func SingleWithDefault[T ~string](question string, defaultValue string, options ...T) (T, error) {
	return singleSelect(question, defaultValue, options)
}

func singleSelect[T ~string](question string, defaultValue string, options []T) (T, error) {
	var zero T

	mapped := make(map[string]T)
	items := make([]radioItem, 0, len(options))
	cursor := 0
	for i, option := range options {
		str := string(option)
		mapped[str] = option
		isDef := defaultValue != "" && str == defaultValue
		items = append(items, radioItem{
			text:      str,
			isDefault: isDef,
		})
		if isDef {
			cursor = i
		}
	}

	p := tea.NewProgram(&singleSelectModel{
		question: question,
		options:  items,
		cursor:   cursor,
		selected: -1,
		help: help.Model{
			ShortSeparator: " • ",
			FullSeparator:  "    ",
			Ellipsis:       "…",
			Styles: help.Styles{
				ShortKey:       lipgloss.NewStyle().Foreground(dimmerColor),
				ShortDesc:      lipgloss.NewStyle().Foreground(dimColor),
				ShortSeparator: lipgloss.NewStyle().Foreground(dimmestColor),
				Ellipsis:       lipgloss.NewStyle().Foreground(dimColor),
				FullKey:        lipgloss.NewStyle().Foreground(dimmerColor),
				FullDesc:       lipgloss.NewStyle().Foreground(dimColor),
				FullSeparator:  lipgloss.NewStyle().Foreground(dimmestColor),
			},
		},
	})

	m, err := p.Run()
	if err != nil {
		return zero, jujuerrors.Annotate(err, "could not start single selection prompt")
	}

	mo := m.(*singleSelectModel)
	if mo.selected < 0 || mo.selected >= len(mo.options) {
		return zero, ErrNoOptionSelected
	}

	return mapped[mo.options[mo.selected].text], nil
}

type singleSelectModel struct {
	question    string
	options     []radioItem
	cursor      int
	selected    int
	quitting    bool
	filtering   bool
	filterInput textinput.Model
	help        help.Model
}

type selectorKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Filter key.Binding
	Cancel key.Binding
}

func (k selectorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Filter, k.Cancel}
}

func (k selectorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Select, k.Filter, k.Cancel}}
}

var defaultSelectorKeys = selectorKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑/↓", "navigate"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("", ""),
		key.WithDisabled(),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
}

var filteringSelectorKeys = selectorKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑/↓", "navigate"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("", ""),
		key.WithDisabled(),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Filter: key.NewBinding(
		key.WithKeys("backspace"),
		key.WithHelp("esc", "clear filter"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("", ""),
		key.WithDisabled(),
	),
}

type radioItem struct {
	text      string
	isDefault bool
}

// filtered returns the indices of options that match the current filter.
func (m *singleSelectModel) filtered() []int {
	val := m.filterInput.Value()
	if val == "" {
		idxs := make([]int, len(m.options))
		for i := range m.options {
			idxs[i] = i
		}
		return idxs
	}
	lower := strings.ToLower(val)
	var idxs []int
	for i, item := range m.options {
		if fuzzyMatch(strings.ToLower(item.text), lower) {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

// fuzzyMatch reports whether text contains all characters of pattern in order.
func fuzzyMatch(text, pattern string) bool {
	pi := 0
	for i := range len(text) {
		if pi < len(pattern) && text[i] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

func (m singleSelectModel) Init() tea.Cmd {
	return nil
}

func (m *singleSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return m, m.handleKeyMsg(typed)
	}
	// Forward non-key messages (e.g. cursor blink) to the text input
	// when filtering is active.
	if m.filtering {
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *singleSelectModel) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	// In filter mode, handle typing and navigation within the filtered list.
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.filterInput.Reset()
			m.filterInput.Blur()
			// Reset cursor to stay in bounds of the full list.
			if m.cursor >= len(m.options) {
				m.cursor = 0
			}
			return nil
		case "ctrl+c":
			m.filtering = false
			m.filterInput.Reset()
			m.filterInput.Blur()
			m.quitting = true
			return tea.Quit
		case "enter":
			visible := m.filtered()
			if len(visible) > 0 {
				m.selected = visible[m.cursor]
			}
			m.filtering = false
			m.filterInput.Reset()
			m.filterInput.Blur()
			m.quitting = true
			return tea.Quit
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return nil
		case "down":
			visible := m.filtered()
			if m.cursor+1 < len(visible) {
				m.cursor++
			}
			return nil
		default:
			// Delegate all other keys (typing, backspace, etc.) to the
			// text input model.
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			visible := m.filtered()
			if m.cursor >= len(visible) {
				m.cursor = max(0, len(visible)-1)
			}
			return cmd
		}
	}

	switch msg.String() {
	case "esc", "ctrl+c":
		m.quitting = true
		return tea.Quit
	case "enter":
		if m.selected < 0 {
			m.selected = m.cursor
		}
		m.quitting = true
		return tea.Quit
	case "/":
		m.filtering = true
		m.filterInput = textinput.New()
		m.filterInput.Prompt = ""
		m.cursor = 0
		return m.filterInput.Focus()
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down":
		if m.cursor+1 < len(m.options) {
			m.cursor++
		}
	}
	return nil
}

func (m *singleSelectModel) View() tea.View {
	var out strings.Builder

	out.WriteString(pipeLineStyle("?"))
	out.WriteByte(' ')
	out.WriteString(m.question)
	out.WriteString(":\n")
	out.WriteString(pipeLineStyle(" "))
	out.WriteByte('\n')

	visible := m.filtered()

	for vi, idx := range visible {
		item := m.options[idx]
		var text string
		var radio string

		isCursor := vi == m.cursor && !m.quitting
		isSelected := m.quitting && m.selected == idx

		if isCursor {
			radio = cursorLineStyle(" ")
			text = colors.PrimaryFg(item.text)
		} else if isSelected {
			radio = selectLineStyle("●")
			text = colors.PrimaryFg(item.text)
		} else {
			radio = pipeLineStyle(" ")
			text = item.text
		}

		if isCursor {
			radio += " ▸" + dimStyle("[")
		} else {
			radio += "   "
		}

		out.WriteString(radio)
		out.WriteString(text)

		if isCursor {
			out.WriteString(dimStyle("]"))
		} else {
			out.WriteByte(' ')
		}

		if item.isDefault {
			out.WriteString(dimmestStyle(" (current)"))
		}

		out.WriteByte('\n')
	}

	out.WriteString(pipeLineStyle(" "))
	out.WriteByte('\n')

	if !m.quitting {
		if m.filtering {
			out.WriteString(pipeLineStyle(" "))
			out.WriteByte(' ')
			out.WriteString(dimStyle("filter: "))
			out.WriteString(m.filterInput.View())
			out.WriteByte('\n')
			out.WriteString(pipeLineStyle(" "))
			out.WriteString("\n")
			out.WriteString(pipeLineStyle(" "))
			out.WriteByte(' ')
			out.WriteString(m.help.View(filteringSelectorKeys))
		} else {
			out.WriteString(pipeLineStyle(" "))
			out.WriteByte(' ')
			out.WriteString(m.help.View(defaultSelectorKeys))
		}
	}

	return tea.NewView(out.String())
}
