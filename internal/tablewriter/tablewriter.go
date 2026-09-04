// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tablewriter

import (
	"bytes"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"

	xio "unikraft.com/x/io"
)

type tableWriter struct {
	w      io.Writer
	buffer []byte

	headers      [][]byte
	cleanheaders [][]byte
	alignments   []Alignment
	rows         [][][]byte
	cleanrows    [][][]byte
}

// TableWriter returns a Writer that formats markdown tables written to it.
// Lines with | characters are treated as table rows. Lines without | characters
// flush the current table and pass through unmodified.
// The first table line is the header row.
// The second table line defines column alignments using markdown syntax (:---, :---:, ---:).
func TableWriter(w io.Writer) xio.WriteFlusher {
	return &tableWriter{w: w}
}

func (t *tableWriter) Write(p []byte) (n int, err error) {
	t.buffer = append(t.buffer, p...)
	lastNewline := bytes.LastIndexByte(t.buffer, '\n')
	if lastNewline == -1 {
		return len(p), nil
	}
	lines := bytes.Lines(t.buffer[:lastNewline+1])
	t.buffer = t.buffer[lastNewline+1:]

	for line := range lines {
		line = bytes.TrimSuffix(line, []byte("\n"))
		line, hasPrefix := bytes.CutPrefix(line, []byte("|"))
		line, hasSuffix := bytes.CutSuffix(line, []byte("|"))
		if !hasPrefix || !hasSuffix {
			if err := t.flushTable(); err != nil {
				return 0, err
			}
			if _, err := t.w.Write(append(line, '\n')); err != nil {
				return 0, err
			}
			continue
		}

		var cells [][]byte
		var cleanCells [][]byte
		for part := range bytes.SplitSeq(line, []byte("|")) {
			trimmed := bytes.TrimSpace(part)
			cells = append(cells, trimmed)
			cleanCells = append(cleanCells, []byte(ansi.Strip(string(trimmed))))
		}

		if t.headers == nil {
			t.headers = cells
			t.cleanheaders = cleanCells
		} else if t.alignments == nil {
			t.alignments = make([]Alignment, min(len(cells), len(t.headers)))
			for i := range min(len(cells), len(t.headers)) {
				t.alignments[i] = parseAlignment(string(cleanCells[i]))
			}
		} else {
			t.rows = append(t.rows, cells)
			t.cleanrows = append(t.cleanrows, cleanCells)
		}
	}

	return len(p), nil
}

type Alignment int

const (
	AlignLeft Alignment = iota
	AlignCenter
	AlignRight
	AlignNone
)

func parseAlignment(spec string) Alignment {
	spec = strings.TrimSpace(spec)
	hasLeft := strings.HasPrefix(spec, ":")
	hasRight := strings.HasSuffix(spec, ":")

	if hasLeft && hasRight {
		return AlignCenter
	} else if hasRight {
		return AlignRight
	} else if hasLeft {
		return AlignLeft
	}
	return AlignNone
}

func (a Alignment) Separator(width int) string {
	switch a {
	case AlignCenter:
		return ":" + strings.Repeat("-", max(0, width-2)) + ":"
	case AlignRight:
		return strings.Repeat("-", max(0, width-1)) + ":"
	case AlignLeft:
		return ":" + strings.Repeat("-", max(0, width-1))
	default:
		return strings.Repeat("-", max(0, width))
	}
}

func (t *tableWriter) flushTable() error {
	if t.headers == nil {
		return nil
	}

	colWidths := make([]int, len(t.headers))
	for i, header := range t.cleanheaders {
		colWidths[i] = ansi.StringWidth(string(header))
	}

	// Ensure minimum width for separator syntax
	for i, align := range t.alignments {
		switch align {
		case AlignCenter:
			colWidths[i] = max(colWidths[i], 3) // :-:
		case AlignLeft, AlignRight:
			colWidths[i] = max(colWidths[i], 2) // :- or -:
		default:
			colWidths[i] = max(colWidths[i], 1) // -
		}
	}

	for _, row := range t.cleanrows {
		for i, cell := range row {
			if i < len(colWidths) {
				colWidths[i] = max(colWidths[i], ansi.StringWidth(string(cell)))
			}
		}
	}

	if err := t.writeRow(t.headers, colWidths, t.cleanheaders); err != nil {
		return err
	}

	if t.alignments != nil {
		separatorRow := make([][]byte, len(t.alignments))
		for i, align := range t.alignments {
			separatorRow[i] = []byte(align.Separator(colWidths[i]))
		}
		if err := t.writeRow(separatorRow, colWidths, nil); err != nil {
			return err
		}
	}

	for i, row := range t.rows {
		if err := t.writeRow(row, colWidths, t.cleanrows[i]); err != nil {
			return err
		}
	}

	t.headers = nil
	t.cleanheaders = nil
	t.alignments = nil
	t.rows = nil
	t.cleanrows = nil

	return nil
}

func (t *tableWriter) writeRow(cells [][]byte, colWidths []int, cleanCells [][]byte) error {
	if cleanCells == nil {
		cleanCells = make([][]byte, len(cells))
		for i, cell := range cells {
			cleanCells[i] = []byte(ansi.Strip(string(cell)))
		}
	}

	var line []byte
	line = append(line, '|')
	for i := range len(colWidths) {
		line = append(line, ' ')

		var cell []byte
		var cleanCell []byte
		if i < len(cells) {
			cell = cells[i]
			cleanCell = cleanCells[i]
		}

		var align Alignment
		if t.alignments != nil && i < len(t.alignments) {
			align = t.alignments[i]
		}

		cleanLen := ansi.StringWidth(string(cleanCell))
		width := colWidths[i]
		padding := width - cleanLen

		switch align {
		case AlignRight:
			line = append(line, bytes.Repeat([]byte(" "), padding)...)
			line = append(line, cell...)
		case AlignCenter:
			leftPad := padding / 2
			rightPad := padding - leftPad
			line = append(line, bytes.Repeat([]byte(" "), leftPad)...)
			line = append(line, cell...)
			line = append(line, bytes.Repeat([]byte(" "), rightPad)...)
		default:
			line = append(line, cell...)
			line = append(line, bytes.Repeat([]byte(" "), padding)...)
		}

		line = append(line, ' ')
		line = append(line, '|')
	}
	line = append(line, '\n')

	_, err := t.w.Write(line)
	return err
}

func (t *tableWriter) Flush() error {
	if len(t.buffer) > 0 {
		if _, err := t.Write([]byte{'\n'}); err != nil {
			return err
		}
	}
	if err := t.flushTable(); err != nil {
		return err
	}
	if tw, ok := t.w.(xio.Flusher); ok {
		return tw.Flush()
	}
	return nil
}
