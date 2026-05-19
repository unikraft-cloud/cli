// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package progdl provides a download progress bar component using bubbletea.
package progdl

import (
	"context"
	"fmt"
	"io"
	"sync"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"unikraft.com/x/colors"
)

const maxWidth = 0 // 0 means no max width limit

var helpStyle = lipgloss.NewStyle().Foreground(colors.Info)

// progressWriter wraps an io.Writer to track bytes written and send progress updates.
type progressWriter struct {
	total      int64
	downloaded int64
	file       io.Writer
	onProgress func(float64)
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.file.Write(p)
	pw.downloaded += int64(n)
	if pw.total > 0 && pw.onProgress != nil {
		pw.onProgress(float64(pw.downloaded) / float64(pw.total))
	}
	return n, err
}

// contextReader wraps an io.ReadCloser to respect context cancellation.
// It starts a single goroutine that closes the underlying reader when the
// context is cancelled, rather than spawning a goroutine per Read call.
type contextReader struct {
	ctx        context.Context
	r          io.ReadCloser
	closeOnce  sync.Once
	cancelFunc context.CancelFunc
}

// newContextReader creates a contextReader that closes r when ctx is cancelled.
func newContextReader(ctx context.Context, r io.ReadCloser) *contextReader {
	// Derive a context so we can stop the cancellation goroutine on Close.
	ctx, cancel := context.WithCancel(ctx)
	cr := &contextReader{
		ctx:        ctx,
		r:          r,
		cancelFunc: cancel,
	}
	// Single cancellation goroutine: closes r when ctx is cancelled.
	go func() {
		<-ctx.Done()
		cr.Close()
	}()
	return cr
}

func (cr *contextReader) Read(p []byte) (int, error) {
	// Simple passthrough. The cancellation goroutine closes r when ctx is
	// cancelled, which should unblock this Read with an error.
	n, err := cr.r.Read(p)
	// If context was cancelled, return context error for clarity.
	if err != nil && cr.ctx.Err() != nil {
		return n, cr.ctx.Err()
	}
	return n, err
}

// Close closes the underlying reader and stops the cancellation goroutine.
func (cr *contextReader) Close() error {
	var err error
	cr.closeOnce.Do(func() {
		cr.cancelFunc() // Stop the cancellation goroutine
		err = cr.r.Close()
	})
	return err
}

// progressMsg is sent when download progress is updated.
type progressMsg float64

// doneMsg is sent when the download is complete.
type doneMsg struct{}

// errMsg is sent when an error occurs.
type errMsg struct{ err error }

// Model represents the progress bar state.
type Model struct {
	progress progress.Model
	percent  float64
	done     bool
	err      error
	width    int
}

// New creates a new progress bar model.
func New() Model {
	return Model{
		progress: progress.New(
			progress.WithScaled(false),
			progress.WithColors(colors.Blue500, colors.Emerald500),
		),
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		if maxWidth > 0 && m.width > maxWidth {
			m.width = maxWidth
		}
		m.progress.SetWidth(m.width)
		return m, nil

	case progressMsg:
		m.percent = float64(msg)
		if m.percent > 1.0 {
			m.percent = 1.0
		}
		cmd := m.progress.SetPercent(m.percent)
		return m, cmd

	case doneMsg:
		m.done = true
		m.percent = 1.0
		cmd := m.progress.SetPercent(1.0)
		return m, tea.Sequence(cmd, tea.Quit)

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	case progress.FrameMsg:
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		return m, cmd

	default:
		return m, nil
	}
}

// View renders the progress bar.
func (m Model) View() tea.View {
	if m.err != nil {
		return tea.NewView(fmt.Sprintf("error: %v\n", m.err))
	}

	if m.done {
		return tea.View{}
	}

	return tea.NewView(fmt.Sprintf("%s %s\n",
		m.progress.View(),
		helpStyle.Render(fmt.Sprintf("%.0f%%", m.percent*100)),
	))
}
