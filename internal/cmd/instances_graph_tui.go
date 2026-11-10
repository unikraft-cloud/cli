// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"unikraft.com/x/colors"
	xio "unikraft.com/x/io"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/tui/graph"
	"unikraft.com/cli/internal/tui/styles"
)

// graphLiveSupported reports whether this build can draw the live chart.
const graphLiveSupported = true

// defaultGraphWidth is the width used until the terminal reports its size.
const defaultGraphWidth = 80

// graphStyle is the style of the live chart. Colors are dropped by the
// renderer when the terminal has none or NO_COLOR is set.
var graphStyle = graph.Style{
	Line: lipgloss.NewStyle().Foreground(colors.Primary),
	Axis: styles.Hint,
	Note: styles.Hint,
}

type (
	graphTickMsg   struct{}
	graphSampleMsg struct {
		at      time.Time
		reading graphReading
		err     error
	}
)

// graphModel is the live chart. It draws from the samples that it holds at the
// size of the last tea.WindowSizeMsg. A tick starts a request, unless one is
// still running, and the ticks come on a fixed schedule. Thus the time between
// two samples does not grow with the time that a request takes.
type graphModel struct {
	ctx     context.Context
	session *graphSession
	// samples is how many polls to take before the chart ends. Zero means
	// until it is interrupted.
	samples int

	width, height int
	fetching      bool
	taken         int
	// err is why the last poll gave no values.
	err error
}

func (m *graphModel) Init() tea.Cmd {
	m.fetching = true
	return tea.Batch(m.fetchCmd(), m.tickCmd())
}

func (m *graphModel) tickCmd() tea.Cmd {
	return tea.Tick(m.session.interval, func(time.Time) tea.Msg { return graphTickMsg{} })
}

func (m *graphModel) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		reading, err := m.session.fetch(m.ctx)
		return graphSampleMsg{at: time.Now(), reading: reading, err: err}
	}
}

func (m *graphModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Keep enough points to fill the chart of a wide terminal. A shrinking
		// window trims the series on the next point.
		m.session.limit = graphLimit(msg.Width)
	case graphSampleMsg:
		m.fetching = false
		m.err = m.session.observe(msg.at, msg.reading, msg.err)
		m.taken++
		if m.samples > 0 && m.taken >= m.samples {
			return m, tea.Quit
		}
		return m, nil
	case graphTickMsg:
		if m.fetching {
			return m, m.tickCmd()
		}
		m.fetching = true
		return m, tea.Batch(m.fetchCmd(), m.tickCmd())
	case tea.KeyPressMsg:
		if k := msg.Keystroke(); k == "ctrl+c" || k == "q" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *graphModel) View() tea.View {
	width := cmp.Or(m.width, defaultGraphWidth)
	height := 0
	if m.height > 0 {
		// Leave one row for the status line. A chart is not taller than it
		// needs to be, and sparklines use the rows that they have.
		height = m.height - 1
		if len(m.session.lines) == 1 {
			height = min(height, graph.PlotHeight+2)
		}
	}
	return tea.NewView(m.session.frame(width, height, graphStyle) + "\n" + m.status(width))
}

// status is the line below the chart.
func (m *graphModel) status(width int) string {
	if m.err != nil {
		line := ansi.Truncate("error: "+cleanLine(m.err.Error()), width, "…")
		return styles.Error.Render(line)
	}
	return styles.Hint.Render(fmt.Sprintf("every %s  ctrl+c to quit", m.session.interval))
}

// runGraphLive shows the chart on a terminal until it is interrupted or the
// requested number of samples is taken.
func runGraphLive(ctx context.Context, stdio config.Stdio, s *graphSession, samples int) error {
	out := xio.Unwrap(stdio.Stdout)
	program := tea.NewProgram(
		&graphModel{ctx: ctx, session: s, samples: samples},
		tea.WithOutput(out),
		tea.WithContext(ctx),
	)
	_, err := program.Run()
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
		err = nil
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return s.failure()
}
