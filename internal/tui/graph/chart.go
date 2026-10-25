// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package graph renders time series as braille line charts and block
// sparklines. Rendering is pure: values, times, sizes and styles go in, and a
// string comes out. Nothing here reads the clock or the terminal.
package graph

import (
	"fmt"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Point is one sample of a series. A point with OK unset is a gap: the sample
// failed or has no value. It is never drawn as zero.
type Point struct {
	T  time.Time
	V  float64
	OK bool
}

// Style holds the styles of a chart. The zero value renders plain text.
type Style struct {
	Line lipgloss.Style
	Axis lipgloss.Style
	Note lipgloss.Style
}

// Options configure Chart.
type Options struct {
	// Title is the first line, such as "my-instance  cpu (%)". The newest
	// value is appended to it.
	Title string
	Unit  Unit
	// Ceiling fixes the top of the axis, unless the data is above it.
	Ceiling float64
	// Width is the width of the frame in cells.
	Width int
	// Height is the height of the frame in rows. Zero means the default
	// plot height.
	Height int
	// Interval is the time between two samples. It sets the time scale of
	// the x axis, where a point sits and where a line breaks.
	Interval time.Duration
	// Loc is the time zone of the x axis labels. Nil means UTC.
	Loc *time.Location
	// Empty is shown after the title while no point has a value.
	Empty string
	Style Style
}

const (
	// MinWidth and MinHeight are the smallest frame that Chart draws.
	MinWidth  = 30
	MinHeight = 5

	// PlotHeight is the number of plot rows of a frame of default height.
	PlotHeight = 10
	// gutter is the value labels plus the axis cell.
	gutter = axisLabelWidth + 1
	// gapFactor is how many intervals apart two points can be and still be
	// joined by a line.
	gapFactor = 1.5
)

// timeSteps are the spacings of the x axis labels.
var timeSteps = []time.Duration{
	10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute,
	10 * time.Minute, 30 * time.Minute, time.Hour,
}

// Chart draws one series as a braille line chart. The newest point is at the
// right edge. The result has no trailing newline.
func Chart(points []Point, o Options) string {
	if o.Width < MinWidth || (o.Height != 0 && o.Height < MinHeight) {
		note := fmt.Sprintf("too small for a chart (needs %dx%d)", MinWidth, MinHeight)
		return o.Style.Note.Render(ansi.Truncate(note, max(o.Width, 1), ""))
	}
	plotW := o.Width - gutter
	plotH := PlotHeight
	if o.Height != 0 {
		plotH = min(PlotHeight, o.Height-2)
	}
	subW := 2 * plotW
	points = visible(points, o.Interval, subW)

	var lines []string
	lines = append(lines, o.Style.Note.Render(ansi.Truncate(headline(points, o), o.Width, "…")))

	top := axisTop(maxValue(points), o.Unit, o.Ceiling)
	canvas := drawCanvas(points, o, top, plotW, plotH)
	labels := axisLabelRows(top, o.Unit, plotH)
	for r := range plotH {
		label, tick := "", "│"
		if l, ok := labels[r]; ok {
			label, tick = l, "┤"
		}
		margin := o.Style.Axis.Render(fmt.Sprintf("%*s%s", axisLabelWidth, label, tick))
		lines = append(lines, margin+o.Style.Line.Render(canvas[r]))
	}
	ticks := strings.TrimRight(strings.Repeat(" ", gutter)+timeLabels(points, o, plotW), " ")
	lines = append(lines, o.Style.Axis.Render(ticks))
	return strings.Join(lines, "\n")
}

func headline(points []Point, o Options) string {
	if len(points) == 0 || !anyOK(points) {
		return strings.TrimRight(o.Title+"  "+o.Empty, " ")
	}
	last := points[len(points)-1]
	if !last.OK {
		return o.Title + "  last -"
	}
	return o.Title + "  last " + FormatReading(last.V, o.Unit)
}

func anyOK(points []Point) bool {
	for _, p := range points {
		if p.OK {
			return true
		}
	}
	return false
}

func maxValue(points []Point) float64 {
	m := 0.0
	for _, p := range points {
		if p.OK {
			m = max(m, p.V)
		}
	}
	return m
}

// axisLabelRows maps plot rows to the value labels of the top, middle and
// bottom.
func axisLabelRows(top float64, unit Unit, plotH int) map[int]string {
	labels := axisLabels([]float64{top, top / 2, 0}, unit)
	return map[int]string{
		0:                 labels[0],
		(4*plotH - 1) / 8: labels[1],
		plotH - 1:         labels[2],
	}
}

// brailleBits maps a dot (column, row inside its cell) to its bit.
var brailleBits = [2][4]uint8{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

// column returns the dot column of a point. The newest point is in the last
// column, and a column is one interval wide. Without an interval, the columns
// follow the index of the point.
func column(points []Point, i int, interval time.Duration, subW int) int {
	if interval <= 0 {
		return subW - len(points) + i
	}
	age := points[len(points)-1].T.Sub(points[i].T)
	return subW - 1 - int(math.Round(float64(age)/float64(interval)))
}

// visible returns the points that have a dot column on the chart.
func visible(points []Point, interval time.Duration, subW int) []Point {
	for i := range points {
		if column(points, i, interval, subW) >= 0 {
			return points[i:]
		}
	}
	return points
}

// drawCanvas draws the series on a grid of braille dots, two per cell
// horizontally and four vertically. A point sits in the dot column of its
// time.
func drawCanvas(points []Point, o Options, top float64, plotW, plotH int) []string {
	cells := make([][]uint8, plotH)
	for r := range cells {
		cells[r] = make([]uint8, plotW)
	}
	set := func(x, y int) {
		if x < 0 || y < 0 || x >= 2*plotW || y >= 4*plotH {
			return
		}
		cells[y/4][x/2] |= brailleBits[x%2][y%4]
	}
	row := func(v float64) int {
		ratio := min(max(v/top, 0), 1)
		return int(math.Round((1 - ratio) * float64(4*plotH-1)))
	}

	for i, p := range points {
		if !p.OK {
			continue
		}
		x, y := column(points, i, o.Interval, 2*plotW), row(p.V)
		set(x, y)
		if i == 0 || !points[i-1].OK || o.joinGap(points[i-1], p) {
			continue
		}
		line(column(points, i-1, o.Interval, 2*plotW), row(points[i-1].V), x, y, set)
	}

	out := make([]string, plotH)
	var sb strings.Builder
	for r, cols := range cells {
		sb.Reset()
		for _, bits := range cols {
			if bits == 0 {
				sb.WriteRune(' ')
				continue
			}
			sb.WriteRune(rune(0x2800) + rune(bits))
		}
		out[r] = strings.TrimRight(sb.String(), " ")
	}
	return out
}

// joinGap reports whether two points are too far apart to be joined.
func (o Options) joinGap(a, b Point) bool {
	return o.Interval > 0 && b.T.Sub(a.T) > time.Duration(float64(o.Interval)*gapFactor)
}

// line calls set for every dot between two dots, ends included.
func line(x0, y0, x1, y1 int, set func(x, y int)) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := sign(x1-x0), sign(y1-y0)
	errTerm := dx + dy
	for {
		set(x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		doubled := 2 * errTerm
		if doubled >= dy {
			errTerm += dy
			x0 += sx
		}
		if doubled <= dx {
			errTerm += dx
			y0 += sy
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// timeLabels builds the row of x axis labels. The time of a dot column is
// derived from the time of the newest point and the interval.
func timeLabels(points []Point, o Options, plotW int) string {
	row := []rune(strings.Repeat(" ", plotW))
	if len(points) == 0 || o.Interval <= 0 {
		return string(row)
	}
	loc := o.Loc
	if loc == nil {
		loc = time.UTC
	}
	right := points[len(points)-1].T
	subW := 2 * plotW

	step, layout := labelStep(o.Interval)
	width := len(layout)
	left := right.Add(-time.Duration(subW-1) * o.Interval)
	_, offset := right.In(loc).Zone()
	stepSec := int64(step / time.Second)

	end := 0
	first := ((left.Unix()+int64(offset))/stepSec)*stepSec - int64(offset)
	for sec := first; ; sec += stepSec {
		tick := time.Unix(sec, 0)
		if tick.After(right) {
			break
		}
		if tick.Before(left) {
			continue
		}
		dot := subW - 1 - int(math.Round(float64(right.Sub(tick))/float64(o.Interval)))
		start := min(dot/2-width/2, plotW-width)
		if start < 0 || (end > 0 && start <= end) {
			continue
		}
		copy(row[start:], []rune(tick.In(loc).Format(layout)))
		end = start + width
	}
	return string(row)
}

// labelStep returns the smallest label spacing that leaves room between two
// labels, and the time layout of its labels.
func labelStep(interval time.Duration) (time.Duration, string) {
	var step time.Duration
	var layout string
	for _, step = range timeSteps {
		layout = "15:04"
		if step < time.Minute {
			layout = "15:04:05"
		}
		cells := float64(step) / float64(2*interval)
		if cells >= float64(len(layout)+2) {
			break
		}
	}
	return step, layout
}
