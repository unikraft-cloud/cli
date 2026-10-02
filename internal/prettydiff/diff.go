// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package prettydiff provides functionality to render diffs from the
// github.com/sergi/go-diff package in a pretty, colored format.
package prettydiff

import (
	"bytes"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/sergi/go-diff/diffmatchpatch"
	"unikraft.com/x/colors"
)

// Render takes a slice of diffs and returns a pretty-printed string
// representation with color coding for additions and deletions.
//
// The diffs are broken down into a line-by-line format, grouped into hunks,
// and rendered with appropriate ANSI color codes for the terminal display.
func Render(diffs []diffmatchpatch.Diff) string {
	lines := splitLines(diffs)

	var buff bytes.Buffer
	lines = groupLines(lines)
	for _, line := range lines {
		line.render(&buff)
	}
	return buff.String()
}

// groupLines groups lines into hunks (so we group deletions next to deletions,
// instead of alternating between deletion + additions)
func groupLines(lines []line) []line {
	var done []line

	var added []line
	var removed []line

	for _, l := range lines {
		switch l.op {
		case diffmatchpatch.DiffInsert:
			added = append(added, l)
		case diffmatchpatch.DiffDelete:
			removed = append(removed, l)
		case diffmatchpatch.DiffEqual:
			done = append(done, removed...)
			removed = nil
			done = append(done, added...)
			added = nil
			done = append(done, l)
		}
	}
	done = append(done, removed...)
	done = append(done, added...)
	return done
}

type line struct {
	op    diffmatchpatch.Operation
	diffs []diffmatchpatch.Diff
}

func splitLines(diffs []diffmatchpatch.Diff) []line {
	var lines []line
	var oldLine []diffmatchpatch.Diff
	var newLine []diffmatchpatch.Diff

	for _, diff := range diffs {
		text := diff.Text

		for len(text) > 0 {
			idx := strings.IndexByte(text, '\n')
			segment := text
			ended := idx != -1
			if ended {
				segment = text[:idx]
			}
			addSegment := true
			if ended && segment == "" && (len(oldLine) > 0 || len(newLine) > 0) {
				// A leading newline in this diff chunk is just the line terminator
				// for a line that already has content; don't add an empty segment.
				addSegment = false
			}
			if addSegment {
				part := diffmatchpatch.Diff{Type: diff.Type, Text: segment}
				if diff.Type != diffmatchpatch.DiffInsert {
					oldLine = append(oldLine, part)
				}
				if diff.Type != diffmatchpatch.DiffDelete {
					newLine = append(newLine, part)
				}
			}
			if !ended {
				break
			}

			// determine where the newline occurred - this affects what gets flushed.
			oldEnded, newEnded := false, false
			switch diff.Type {
			case diffmatchpatch.DiffEqual:
				oldEnded = true
				newEnded = true
			case diffmatchpatch.DiffDelete:
				oldEnded = true
			case diffmatchpatch.DiffInsert:
				newEnded = true
			}
			lines = flushLines(lines, &oldLine, &newLine, oldEnded, newEnded)
			text = text[idx+1:]
		}
	}

	if len(oldLine) > 0 || len(newLine) > 0 {
		lines = flushLines(lines, &oldLine, &newLine, len(oldLine) > 0, len(newLine) > 0)
	}

	return lines
}

func flushLines(lines []line, oldLine, newLine *[]diffmatchpatch.Diff, oldEnded, newEnded bool) []line {
	if !oldEnded && !newEnded {
		return lines
	}

	oldText := lineText(*oldLine)
	newText := lineText(*newLine)
	if oldEnded && newEnded {
		if oldText == newText {
			// Both lines ended and have the same text - output as equal
			lines = append(lines, line{
				op: diffmatchpatch.DiffEqual,
				diffs: []diffmatchpatch.Diff{{
					Type: diffmatchpatch.DiffEqual,
					Text: oldText,
				}},
			})
			*oldLine = nil
			*newLine = nil
			return lines
		}
	} else if oldEnded && !newEnded {
		// Old line ended (from a delete newline), new line hasn't.
		// If they have the same text, the new side just hasn't seen its newline
		// yet. We should output as equal and clear both to avoid a spurious
		// delete+insert pair.
		if oldText == newText && oldText != "" {
			lines = append(lines, line{
				op: diffmatchpatch.DiffEqual,
				diffs: []diffmatchpatch.Diff{{
					Type: diffmatchpatch.DiffEqual,
					Text: oldText,
				}},
			})
			*oldLine = nil
			*newLine = nil
			return lines
		}
	} else if !oldEnded && newEnded {
		// New line ended (from an insert newline), old line hasn't.
		// Similar logic - if they match, output as equal.
		if oldText == newText && newText != "" {
			lines = append(lines, line{
				op: diffmatchpatch.DiffEqual,
				diffs: []diffmatchpatch.Diff{{
					Type: diffmatchpatch.DiffEqual,
					Text: newText,
				}},
			})
			*oldLine = nil
			*newLine = nil
			return lines
		}
	}

	if oldEnded {
		if len(*oldLine) > 0 {
			lines = append(lines, line{op: diffmatchpatch.DiffDelete, diffs: *oldLine})
		}
		*oldLine = nil
	}
	if newEnded {
		if len(*newLine) > 0 {
			lines = append(lines, line{op: diffmatchpatch.DiffInsert, diffs: *newLine})
		}
		*newLine = nil
	}
	return lines
}

func lineText(lineDiffs []diffmatchpatch.Diff) string {
	var buff strings.Builder
	for _, diff := range lineDiffs {
		buff.WriteString(diff.Text)
	}
	return buff.String()
}

func (line line) render(buff *bytes.Buffer) {
	switch line.op {
	case diffmatchpatch.DiffEqual:
		buff.WriteString("  ")
		for _, d := range line.diffs {
			buff.WriteString(d.Text)
		}
		buff.WriteString("\n")
	case diffmatchpatch.DiffDelete:
		buff.WriteString(lineRemoveStyle.String())
		buff.WriteString("- ")
		for _, d := range line.diffs {
			if d.Type == diffmatchpatch.DiffDelete {
				buff.WriteString(wordRemoveStyle.String())
				buff.WriteString(d.Text)
				buff.WriteString(lineRemoveStyle.String())
			} else {
				buff.WriteString(d.Text)
			}
		}
		buff.WriteString(ansi.EraseLineRight)
		buff.WriteString(resetStyle.String())
		buff.WriteString("\n")
	case diffmatchpatch.DiffInsert:
		buff.WriteString(lineAddStyle.String())
		buff.WriteString("+ ")
		for _, d := range line.diffs {
			if d.Type == diffmatchpatch.DiffInsert {
				buff.WriteString(wordAddStyle.String())
				buff.WriteString(d.Text)
				buff.WriteString(lineAddStyle.String())
			} else {
				buff.WriteString(d.Text)
			}
		}
		buff.WriteString(ansi.EraseLineRight)
		buff.WriteString(resetStyle.String())
		buff.WriteString("\n")
	}
}

// ANSI color codes for backgrounds
var (
	// combined
	lineRemoveColor = colors.AdaptiveColor{Light: colors.Rose200, Dark: colors.Rose900}
	wordRemoveColor = colors.AdaptiveColor{Light: colors.Rose300, Dark: colors.Rose700}
	lineAddColor    = colors.AdaptiveColor{Light: colors.Emerald200, Dark: colors.Emerald900}
	wordAddColor    = colors.AdaptiveColor{Light: colors.Emerald300, Dark: colors.Emerald700}

	// use ansi styles explicitly, since lipgloss styles don't nest nicely (they
	// emit resets) and we might have input with existing ANSI codes that we
	// don't want to reset.
	resetStyle      = ansi.NewStyle(ansi.AttrDefaultBackgroundColor)
	lineRemoveStyle = ansi.NewStyle().BackgroundColor(colors.Profile.Convert(lineRemoveColor))
	lineAddStyle    = ansi.NewStyle().BackgroundColor(colors.Profile.Convert(lineAddColor))
	wordRemoveStyle = ansi.NewStyle().BackgroundColor(colors.Profile.Convert(wordRemoveColor))
	wordAddStyle    = ansi.NewStyle().BackgroundColor(colors.Profile.Convert(wordAddColor))
)
