// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tui

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"unikraft.com/cli/internal/tui/styles"
	"unikraft.com/cli/internal/tui/uitui"
)

const (
	readerPollInterval   = 100 * time.Millisecond
	horizontalScrollStep = 4
)

// ReaderFunc is a function that returns a reader. Used for lazy initialization.
type ReaderFunc func(ctx context.Context) (io.ReadCloser, error)

// readerContentMsg is sent when new content is available from the reader
type readerContentMsg struct{}

// readerReadyMsg is sent when the reader is ready
type readerReadyMsg struct {
	reader io.ReadCloser
	err    error
}

// ReaderPanel is a panel that streams content from an io.Reader into a scrollable view.
// It supports follow mode (auto-scroll to bottom), manual scrolling (vertical and horizontal),
// and lazy reader initialization via ReaderFunc.
type ReaderPanel struct {
	title      string
	reader     io.ReadCloser
	readerFunc ReaderFunc
	follow     bool

	viewport viewport.Model
	buf      bytes.Buffer
	mu       sync.Mutex

	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	ready   bool
	lastLen int

	width   int
	height  int
	focused bool
	err     error
}

// NewReaderPanelFunc creates a new panel that will lazily fetch its reader using the provided function.
// This is useful when the reader requires async initialization (e.g., fetching from an API).
func NewReaderPanelFunc(ctx context.Context, title string, readerFunc ReaderFunc, follow bool) *ReaderPanel {
	readCtx, cancel := context.WithCancel(ctx)
	return &ReaderPanel{
		title:      title,
		readerFunc: readerFunc,
		follow:     follow,
		viewport:   viewport.New(),
		ctx:        readCtx,
		cancel:     cancel,
	}
}

func (p *ReaderPanel) Init() tea.Cmd {
	// If we have a readerFunc, fetch the reader lazily
	if p.readerFunc != nil && !p.ready {
		return p.fetchReader()
	}
	// Otherwise start reading immediately
	if !p.started && p.reader != nil {
		p.started = true
		go p.readLoop()
	}
	return p.pollCmd()
}

func (p *ReaderPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.viewport.SetWidth(msg.Width)
		p.viewport.SetHeight(max(0, msg.Height))
		p.updateContent()
		if p.follow {
			p.viewport.GotoBottom()
		}
		return p, nil

	case uitui.PanelFocusMsg:
		p.focused = msg.Focused
		return p, nil

	case readerReadyMsg:
		if msg.err != nil {
			p.mu.Lock()
			p.err = msg.err
			p.mu.Unlock()
			return p, nil
		}
		p.reader = msg.reader
		p.ready = true
		if !p.started {
			p.started = true
			go p.readLoop()
		}
		return p, p.pollCmd()

	case readerContentMsg:
		p.mu.Lock()
		newContent := p.buf.Len() != p.lastLen
		p.lastLen = p.buf.Len()
		p.mu.Unlock()

		if newContent {
			p.updateContent()
			if p.follow {
				p.viewport.GotoBottom()
			}
		}
		return p, p.pollCmd()

	case tea.KeyPressMsg:
		if !p.focused {
			return p, nil
		}
		switch msg.String() {
		case "home", "g":
			// Go to top
			p.follow = false
			p.viewport.GotoTop()
			return p, nil
		case "end", "G":
			// Enable follow mode and go to bottom
			p.viewport.GotoBottom()
			p.follow = true
			return p, nil
		default:
			// Scrolling updates follow if we reach bottom.
			var cmd tea.Cmd
			p.viewport, cmd = p.viewport.Update(msg)
			p.follow = p.viewport.AtBottom()
			return p, cmd
		}
	}

	return p, nil
}

func (p *ReaderPanel) View() tea.View {
	if p.height <= 0 || p.width <= 0 {
		return tea.NewView("")
	}

	p.mu.Lock()
	err := p.err
	p.mu.Unlock()

	if err != nil {
		body := styles.Error.Render("Error: " + err.Error())
		return tea.NewView(body)
	}

	if !p.ready {
		body := styles.Hint.Render("Connecting...")
		return tea.NewView(body)
	}

	p.mu.Lock()
	isEmpty := p.buf.Len() == 0
	p.mu.Unlock()

	if isEmpty {
		if p.follow {
			return tea.NewView(p.trailingEllipsis(""))
		}
		body := styles.Hint.Render("Waiting for content...")
		return tea.NewView(body)
	}

	view := p.viewport.View()
	if p.follow {
		view = p.trailingEllipsis(view)
	}
	return tea.NewView(view)
}

func (p *ReaderPanel) trailingEllipsis(view string) string {
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	if p.height > 0 && len(lines) < p.height {
		lines = append(lines, make([]string, p.height-len(lines))...)
	}
	lastNonEmpty := -1
	for i, line := range slices.Backward(lines) {
		if strings.TrimSpace(line) != "" {
			lastNonEmpty = i
			break
		}
	}
	if lastNonEmpty == len(lines)-1 {
		return view
	}
	idx := lastNonEmpty + 1
	if idx < 0 || idx >= len(lines) {
		return view
	}
	if strings.TrimSpace(lines[idx]) != "" {
		return view
	}
	lines[idx] = styles.Hint.Render("...")
	return strings.Join(lines, "\n")
}

func (p *ReaderPanel) Breadcrumb() string {
	return p.title
}

func (p *ReaderPanel) Actions() []uitui.Action {
	return nil
}

func (p *ReaderPanel) Close() error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.reader != nil {
		_ = p.reader.Close()
	}
	return nil
}

func (p *ReaderPanel) fetchReader() tea.Cmd {
	return func() tea.Msg {
		reader, err := p.readerFunc(p.ctx)
		return readerReadyMsg{reader: reader, err: err}
	}
}

func (p *ReaderPanel) pollCmd() tea.Cmd {
	return tea.Tick(readerPollInterval, func(time.Time) tea.Msg {
		return readerContentMsg{}
	})
}

func (p *ReaderPanel) readLoop() {
	if p.reader == nil {
		return
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		n, err := p.reader.Read(buf)
		if n > 0 {
			p.mu.Lock()
			// Replace tabs with spaces for consistent display
			data := strings.ReplaceAll(string(buf[:n]), "\t", "    ")
			p.buf.WriteString(data)
			p.mu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				p.mu.Lock()
				p.err = err
				p.mu.Unlock()
			}
			return
		}
	}
}

func (p *ReaderPanel) updateContent() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.buf.Len() == 0 {
		p.viewport.SetContent("")
		return
	}

	p.viewport.SetContent(p.buf.String())
}
