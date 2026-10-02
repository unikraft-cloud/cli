// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package monkey provides an animated ASCII art monkey using bubbletea.
package monkey

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"unikraft.com/x/colors"
)

const (
	// LinesPerFrame is the number of lines in each animation frame.
	LinesPerFrame = 3

	// DefaultFrameRate is the default animation speed.
	DefaultFrameRate = 150 * time.Millisecond
)

// Color scheme for the monkey animation.
var (
	BodyStyle = lipgloss.NewStyle().Bold(true).Foreground(
		colors.AdaptiveColor{
			Light: lipgloss.Color("#6B7280"), // neutral gray-500
			Dark:  lipgloss.Color("#9CA3AF"), // gray-400 (brighter for dark bg)
		},
	)

	FaceStyle = lipgloss.NewStyle().Foreground(
		colors.AdaptiveColor{
			Light: lipgloss.Color("#111827"), // near-black
			Dark:  lipgloss.Color("#F9FAFB"), // near-white (softer than pure white)
		},
	)

	EyeStyle = lipgloss.NewStyle().Foreground(
		colors.AdaptiveColor{
			Light: lipgloss.Color("#4B5563"), // gray-600
			Dark:  lipgloss.Color("#D1D5DB"), // gray-300
		},
	)

	MouthStyle = lipgloss.NewStyle().Foreground(
		colors.AdaptiveColor{
			Light: lipgloss.Color("#1F2933"), // slightly softer than pure black
			Dark:  lipgloss.Color("#E5E7EB"), // softer than face
		},
	)
)

// frame represents a single animation frame with 3 lines.
type frame [LinesPerFrame]string

// frames contains all animation frames for the monkey.
// Each frame is 3 lines tall and represents a different pose.
//
//nolint:lll // ASCII art frames are intentionally long/unwrapped.
var frames = []frame{
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "_") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".--'") + "       ",
		"   " + s(BodyStyle, "(| |)_/") + "           ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "o") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".--.") + "       ",
		"   " + s(BodyStyle, "(| |)_/") + "           ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "_") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".-.") + "        ",
		"   " + s(BodyStyle, "(| |)_/") + "   " + s(BodyStyle, "`") + "       ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "o") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".--.") + "       ",
		"   " + s(BodyStyle, "(| |)_/") + "           ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "_") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".--'") + "       ",
		"   " + s(BodyStyle, "(| |)_/") + "           ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "_") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".--.") + "       ",
		"   " + s(BodyStyle, "(| |)_/") + "           ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "_") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".-.") + "        ",
		"   " + s(BodyStyle, "(| |)_/") + "   " + s(BodyStyle, "`") + "       ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "_") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".--.") + "       ",
		"   " + s(BodyStyle, "(| |)_/") + "           ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c-") + s(MouthStyle, "_") + s(FaceStyle, "-o") + "  " + s(BodyStyle, ".--'") + "       ",
		"   " + s(BodyStyle, "(| |)_/") + "           ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "_") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".--.") + "       ",
		"   " + s(BodyStyle, "(| |)_/") + "           ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "_") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".-.") + "        ",
		"   " + s(BodyStyle, "(| |)_/") + "   " + s(BodyStyle, "`") + "       ",
	},
	{
		"     " + s(BodyStyle, "_") + "               ",
		"   " + s(FaceStyle, "c") + s(EyeStyle, "'") + s(MouthStyle, "_") + s(EyeStyle, "'") + s(FaceStyle, "o") + "  " + s(BodyStyle, ".--.") + "       ",
		"   " + s(BodyStyle, "(| |)_/") + "           ",
	},
	{
		s(BodyStyle, ".---") + "    " + s(BodyStyle, "_") + "            ",
		s(BodyStyle, "`--,___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + "          ",
		"   " + s(BodyStyle, `(,--( \`) + "           ",
	},
	{
		s(BodyStyle, ".--") + "      " + s(BodyStyle, "_") + "           ",
		s(BodyStyle, "`---,___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + "         ",
		"    " + s(BodyStyle, `( \-(,`) + "           ",
	},
	{
		s(BodyStyle, ".-") + "        " + s(BodyStyle, "_") + "          ",
		s(BodyStyle, "`---'\\___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + "        ",
		"     " + s(BodyStyle, `(,--( \`) + "         ",
	},
	{
		s(BodyStyle, ".") + "    " + s(BodyStyle, "_") + "     " + s(BodyStyle, "_") + "         ",
		s(BodyStyle, "`---'") + " " + s(BodyStyle, "\\___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + "       ",
		"      " + s(BodyStyle, `( \-(,`) + "         ",
	},
	{
		"     " + s(BodyStyle, "_") + "      " + s(BodyStyle, "_") + "        ",
		s(BodyStyle, "`---'") + " " + s(BodyStyle, "`,___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + "      ",
		"       " + s(BodyStyle, `(,--( \`) + "       ",
	},
	{
		"     " + s(BodyStyle, "_") + "       " + s(BodyStyle, "_") + "       ",
		" " + s(BodyStyle, "---'") + " " + s(BodyStyle, "`-,___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + "     ",
		"        " + s(BodyStyle, `( \-(,`) + "       ",
	},
	{
		"     " + s(BodyStyle, "_") + "        " + s(BodyStyle, "_") + "      ",
		"  " + s(BodyStyle, "--'") + " " + s(BodyStyle, "`--,___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + "    ",
		"         " + s(BodyStyle, `(,--( \`) + "     ",
	},
	{
		"     " + s(BodyStyle, "_") + "         " + s(BodyStyle, "_") + "     ",
		"   " + s(BodyStyle, "-'") + " " + s(BodyStyle, "`---,___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + "   ",
		"          " + s(BodyStyle, `( \-(,`) + "     ",
	},
	{
		"     " + s(BodyStyle, "_") + "          " + s(BodyStyle, "_") + "    ",
		"    " + s(BodyStyle, "'") + " " + s(BodyStyle, "`---'\\___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + "  ",
		"           " + s(BodyStyle, `(,--( \`) + "   ",
	},
	{
		"     " + s(BodyStyle, "_") + "     " + s(BodyStyle, "_") + "     " + s(BodyStyle, "_") + "   ",
		"      " + s(BodyStyle, "`---'") + " " + s(BodyStyle, "\\___") + s(FaceStyle, "c ") + s(EyeStyle, `"`) + s(MouthStyle, ".") + " ",
		"            " + s(BodyStyle, `( \-(,`) + "   ",
	},
	{
		"           " + s(BodyStyle, "_") + "    " + s(BodyStyle, "_") + "    ",
		"      " + s(BodyStyle, "`---'") + " " + s(BodyStyle, "|") + " " + s(FaceStyle, "c") + s(BodyStyle, "   ") + s(FaceStyle, "o") + "  ",
		"            " + s(BodyStyle, `\_(|,|)`) + "  ",
	},
	{
		"             " + s(BodyStyle, "_") + "  " + s(BodyStyle, ".---."),
		"           " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___,-'"),
		"            " + s(BodyStyle, "/ )--,)") + "  ",
	},
	{
		"            " + s(BodyStyle, "_") + "    " + s(BodyStyle, "---."),
		"          " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___,--'"),
		"            " + s(BodyStyle, ",)-/ )") + "   ",
	},
	{
		"           " + s(BodyStyle, "_") + "      " + s(BodyStyle, "--."),
		"         " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___,---'"),
		"          " + s(BodyStyle, "/ )--,)") + "    ",
	},
	{
		"          " + s(BodyStyle, "_") + "        " + s(BodyStyle, "-."),
		"        " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___/`---'"),
		"          " + s(BodyStyle, ",)-/ )") + "     ",
	},
	{
		"         " + s(BodyStyle, "_") + "     " + s(BodyStyle, "_") + "    " + s(BodyStyle, "."),
		"       " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___/") + " " + s(BodyStyle, "`---'"),
		"        " + s(BodyStyle, "/ )--,)") + "      ",
	},
	{
		"        " + s(BodyStyle, "_") + "      " + s(BodyStyle, "_") + "     ",
		"      " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___,'") + " " + s(BodyStyle, "`---'"),
		"        " + s(BodyStyle, ",)-/ )") + "       ",
	},
	{
		"       " + s(BodyStyle, "_") + "       " + s(BodyStyle, "_") + "     ",
		"     " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___,-'") + " " + s(BodyStyle, "`---") + " ",
		"      " + s(BodyStyle, "/ )--,)") + "        ",
	},
	{
		"      " + s(BodyStyle, "_") + "        " + s(BodyStyle, "_") + "     ",
		"    " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___,--'") + " " + s(BodyStyle, "`--") + "  ",
		"      " + s(BodyStyle, ",)-/ )") + "         ",
	},
	{
		"     " + s(BodyStyle, "_") + "         " + s(BodyStyle, "_") + "     ",
		"   " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___,---'") + " " + s(BodyStyle, "`-") + "   ",
		"     " + s(BodyStyle, "/ )-,)") + "          ",
	},
	{
		"    " + s(BodyStyle, "_") + "          " + s(BodyStyle, "_") + "     ",
		"  " + s(MouthStyle, ".") + s(EyeStyle, `"`) + s(FaceStyle, " o") + s(BodyStyle, "___,----'") + " " + s(BodyStyle, "`") + "    ",
		"    " + s(BodyStyle, ",)-/ )") + "           ",
	},
}

// FrameCount returns the total number of animation frames.
func FrameCount() int {
	return len(frames)
}

// s is a shorthand helper that applies a lipgloss style to text.
func s(style lipgloss.Style, text string) string {
	return style.Render(text)
}

// tickMsg is sent when the animation should advance to the next frame.
type tickMsg struct{}

// Model represents the animated monkey component.
type Model struct {
	frame     int
	frameRate time.Duration
	playing   bool
}

// New creates a new monkey animation model with default settings.
func New() Model {
	return Model{
		frame:     0,
		frameRate: DefaultFrameRate,
		playing:   true,
	}
}

// WithFrameRate sets the animation frame rate.
func (m Model) WithFrameRate(d time.Duration) Model {
	m.frameRate = d
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	if m.playing {
		return m.tick()
	}
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg.(type) {
	case tickMsg:
		if !m.playing {
			return m, nil
		}
		m.frame = (m.frame + 1) % len(frames)
		return m, m.tick()
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	f := frames[m.frame]
	return strings.Join(f[:], "\n")
}

// Play starts the animation.
func (m *Model) Play() tea.Cmd {
	m.playing = true
	return m.tick()
}

// Pause stops the animation.
func (m *Model) Pause() {
	m.playing = false
}

// Toggle toggles the animation play/pause state.
func (m *Model) Toggle() tea.Cmd {
	if m.playing {
		m.Pause()
		return nil
	}
	return m.Play()
}

// IsPlaying returns whether the animation is currently playing.
func (m Model) IsPlaying() bool {
	return m.playing
}

// SetFrame sets the current frame index.
func (m *Model) SetFrame(index int) {
	if index >= 0 && index < len(frames) {
		m.frame = index
	}
}

// Frame returns the current frame index.
func (m Model) Frame() int {
	return m.frame
}

// Width returns the width of the animation in characters.
func (m Model) Width() int {
	if len(frames) == 0 || len(frames[0]) == 0 {
		return 0
	}
	// All frames have the same width
	return lipgloss.Width(frames[0][0])
}

// Height returns the height of the animation in lines.
func (m Model) Height() int {
	return LinesPerFrame
}

// tick returns a command that sends a tickMsg after the frame rate duration.
func (m Model) tick() tea.Cmd {
	return tea.Tick(m.frameRate, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}
