// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package watcher

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	xio "unikraft.com/cli/internal/x/io"
)

type watchModel struct {
	underlying  io.Writer
	render      func(io.Writer) error
	output      string
	lastRefresh time.Time
	interval    time.Duration
	err         error
}

type watchRenderMsg struct {
	output string
	err    error
}

type watchTickMsg struct{}

type watchStatusMsg struct{}

func (model watchModel) Init() tea.Cmd {
	return tea.Batch(
		model.watchRenderCmd(),
		model.watchStatusTickCmd(),
	)
}

func (model watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case watchRenderMsg:
		if msg.err != nil {
			model.err = msg.err
			return model, tea.Quit
		}
		model.output = msg.output
		model.lastRefresh = time.Now()
		return model, tea.Tick(model.interval, func(time.Time) tea.Msg {
			return watchTickMsg{}
		})
	case watchTickMsg:
		return model, model.watchRenderCmd()
	case watchStatusMsg:
		return model, model.watchStatusTickCmd()
	case tea.KeyPressMsg:
		if msg.Keystroke() == "ctrl+c" {
			return model, tea.Quit
		}
	}
	return model, nil
}

func (model watchModel) View() tea.View {
	if model.output == "" {
		return tea.NewView("")
	}

	output := strings.TrimSuffix(model.output, "\n")
	if model.lastRefresh.IsZero() {
		return tea.NewView(output)
	}

	elapsed := time.Since(model.lastRefresh).Seconds()
	status := fmt.Sprintf("last refreshed %.1fs ago", elapsed)
	status = lipgloss.NewStyle().Italic(true).Faint(true).Render(status)

	return tea.NewView(output + "\n" + status)
}

func (model watchModel) watchRenderCmd() tea.Cmd {
	return func() tea.Msg {
		var buffer bytes.Buffer
		err := model.render(wrapFdWriter(&buffer, model.underlying))
		return watchRenderMsg{output: buffer.String(), err: err}
	}
}

func (model watchModel) watchStatusTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return watchStatusMsg{}
	})
}

func wrapFdWriter(w io.Writer, fdw io.Writer) io.Writer {
	if fdw, ok := xio.Unwrap(fdw).(interface{ Fd() uintptr }); ok {
		return fdWriter{Writer: w, fd: fdw.Fd()}
	}
	return w
}

type fdWriter struct {
	io.Writer
	fd uintptr
}

func (w fdWriter) Fd() uintptr {
	return w.fd
}
