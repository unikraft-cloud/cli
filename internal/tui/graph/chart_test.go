// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package graph

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const step2s = 2 * time.Second

var t0 = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

// series builds points that are interval apart. NaN makes a gap.
func series(interval time.Duration, values ...float64) []Point {
	points := make([]Point, len(values))
	for i, v := range values {
		points[i] = Point{T: t0.Add(time.Duration(i) * interval), V: v, OK: !math.IsNaN(v)}
	}
	return points
}

// small is a 30x7 frame with a fixed axis from 0 to 8.
func small() Options {
	return Options{Title: "t", Width: MinWidth, Height: 7, Interval: step2s, Loc: time.UTC, Ceiling: 8}
}

// frame drops the newline that follows the opening backtick of an expected frame.
func frame(s string) string { return strings.TrimPrefix(s, "\n") }

// plotCells returns the plot part of a chart row, without the value labels.
func plotCells(row string) string { return strings.TrimSpace(string([]rune(row)[gutter:])) }

func TestChartFlatZero(t *testing.T) {
	t.Parallel()
	o := small()
	o.Title, o.Ceiling = "my-instance  conns", 0
	lines := strings.Split(Chart(series(step2s, 0, 0, 0, 0), o), "\n")
	require.Len(t, lines, 7)
	assert.Equal(t, "my-instance  conns  last 0", lines[0])
	assert.Equal(t, "    1┤", lines[1], "a flat zero line keeps a minimum top")
	assert.Equal(t, "  0.5┤", lines[3])
	for _, row := range lines[1:5] {
		assert.Empty(t, plotCells(row), "the line is on the bottom row only")
	}
	assert.True(t, strings.HasPrefix(lines[5], "    0┤"))
	assert.NotEmpty(t, plotCells(lines[5]))
}

func TestChartJoinsPointsOneStepApart(t *testing.T) {
	t.Parallel()
	want := frame(`
t  last 8
    8┤                      ⢸⢸
     │                      ⢸⢸
    4┤                      ⡜⡜
     │                      ⡇⡇
    0┤                      ⡇⡇
           11:59         12:00
`)
	assert.Equal(t, want, Chart(series(step2s, 0, 8, 0, 8), small())+"\n")
}

func TestChartBreaksLineAtFailedSample(t *testing.T) {
	t.Parallel()
	want := frame(`
t  last 8
    8┤                      ⢸⠈
     │                      ⢸
    4┤                      ⡜
     │                      ⡇
    0┤                      ⡇
           11:59         12:00
`)
	assert.Equal(t, want, Chart(series(step2s, 0, 8, math.NaN(), 8), small())+"\n")
}

func TestChartBreaksLineAtLateSample(t *testing.T) {
	t.Parallel()
	late := series(step2s, 0, 8)
	late[1].T = late[1].T.Add(3 * time.Second)
	lines := strings.Split(Chart(late, small()), "\n")
	require.Len(t, lines, 7)
	assert.NotEmpty(t, plotCells(lines[1]), "the late point is a dot on the top row")
	assert.NotEmpty(t, plotCells(lines[5]), "the first point is a dot on the bottom row")
	for _, row := range lines[2:5] {
		assert.Empty(t, plotCells(row), "points more than 1.5 intervals apart are two dots and no line")
	}

	joined := strings.Split(Chart(series(step2s, 0, 8), small()), "\n")
	assert.NotEmpty(t, plotCells(joined[3]), "points one step apart are joined")
}

func TestChartPlacesPointsByTime(t *testing.T) {
	t.Parallel()
	// Ten samples 5 s apart on a 2 s scale cover 45 s, about 23 dot columns.
	points := series(5*time.Second, 1, 2, 3, 4, 5, 6, 7, 8, 7, 6)
	subW := 2 * (MinWidth - gutter)
	assert.Equal(t, subW-1, column(points, len(points)-1, step2s, subW))
	assert.Equal(t, subW-1-23, column(points, 0, step2s, subW), "the oldest point is 45 s, or 22.5 intervals, back")

	o := small()
	o.Ceiling = 0
	lines := strings.Split(Chart(points, o), "\n")
	plot := lines[1 : len(lines)-1]
	first, last := math.MaxInt, 0
	for _, l := range plot {
		for i, r := range []rune(l)[gutter:] {
			if r != ' ' {
				first, last = min(first, i), max(last, i)
			}
		}
	}
	assert.InDelta(t, 12, last-first+1, 1, "the chart spans 45 s and not 18 s")
}

func TestChartDropsPointsOlderThanTheWindow(t *testing.T) {
	t.Parallel()
	points := series(step2s, 9, 1, 2)
	points[0].T = points[0].T.Add(-10 * time.Minute)
	o := small()
	o.Ceiling = 0
	assert.Contains(t, Chart(points, o), "    2┤", "a point out of the window does not set the axis")

	// A chart keeps only the newest points that fit its width.
	values := make([]float64, 500)
	values[len(values)-1] = 5
	got := Chart(series(step2s, values...), o)
	assert.Contains(t, got, "t  last 5")
	for line := range strings.SplitSeq(got, "\n") {
		assert.LessOrEqual(t, len([]rune(line)), o.Width)
	}
}

func TestChartWithoutValuesShowsNote(t *testing.T) {
	t.Parallel()
	o := small()
	o.Empty = "waiting for a second sample"
	assert.Equal(t, "t  waiting for a second sample", strings.SplitN(Chart(nil, o), "\n", 2)[0])
	assert.Equal(t, "t  waiting for a second sample", strings.SplitN(Chart(series(step2s, math.NaN()), o), "\n", 2)[0])
}

func TestChartMiddleLabelIsOnItsLine(t *testing.T) {
	t.Parallel()
	for _, height := range []int{0, 6, 8, 7} {
		o := Options{Title: "t", Unit: UnitPercent, Ceiling: 100, Width: 40, Height: height, Interval: step2s, Loc: time.UTC}
		lines := strings.Split(Chart(series(step2s, 50, 50, 50, 50, 50, 50), o), "\n")
		var found bool
		for _, line := range lines {
			if strings.HasPrefix(line, "   50┤") {
				found = true
				assert.NotEmpty(t, plotCells(line), "height %d: the 50 label is on the row of the 50 line", height)
			}
		}
		assert.True(t, found, "height %d: the chart has a 50 label", height)
	}
}

func TestChartKeepsLastAfterTheValuesLeftTheWindow(t *testing.T) {
	t.Parallel()
	o := small()
	o.Width, o.Empty = 80, "waiting for the first sample"
	values := []float64{1, 2, 3}
	for range 148 {
		values = append(values, math.NaN())
	}
	head, _, _ := strings.Cut(Chart(series(step2s, values...), o), "\n")
	assert.Equal(t, "t  last -", head)

	// Values that left the points given are remembered by the caller.
	gaps := series(step2s, math.NaN(), math.NaN())
	assert.Contains(t, Chart(gaps, o), "waiting for the first sample")
	o.HadValue = true
	head, _, _ = strings.Cut(Chart(gaps, o), "\n")
	assert.Equal(t, "t  last -", head)
}

func TestChartBytesUseIECLabels(t *testing.T) {
	t.Parallel()
	o := Options{Title: "m", Unit: UnitBytes, Width: 40, Height: 7, Interval: 30 * time.Second, Loc: time.UTC}
	got := Chart(series(30*time.Second, 200<<20, 300<<20, 600<<20, 900<<20, 700<<20), o)
	assert.Contains(t, got, "m  last 700 MiB")
	assert.Contains(t, got, "  1Gi┤", "a top near 1000 MiB is 1 GiB")
	assert.Contains(t, got, "512Mi┤")
	assert.Contains(t, got, "    0┤")
}

func TestChartTooSmall(t *testing.T) {
	t.Parallel()
	tests := map[string]Options{
		"narrow": {Width: MinWidth - 1, Height: 10},
		"short":  {Width: 80, Height: MinHeight - 1},
	}
	for name, o := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := Chart(series(step2s, 1, 2), o)
			assert.NotContains(t, got, "\n")
			assert.Contains(t, got, "too small")
			assert.LessOrEqual(t, len([]rune(got)), max(o.Width, 1))
		})
	}
}

func TestChartHeight(t *testing.T) {
	t.Parallel()
	o := small()
	o.Height = 0
	assert.Len(t, strings.Split(Chart(series(step2s, 1), o), "\n"), PlotHeight+2, "the default has a title, 10 plot rows and an x axis row")
	o.Height = MinHeight
	assert.Len(t, strings.Split(Chart(series(step2s, 1), o), "\n"), MinHeight)
}

func TestPickStep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		interval time.Duration
		step     time.Duration
		layout   string
	}{
		{time.Second, 30 * time.Second, "15:04:05"},
		{step2s, time.Minute, "15:04"},
		{30 * time.Second, 10 * time.Minute, "15:04"},
		{time.Hour, time.Hour, "15:04"},
	}
	for _, tt := range tests {
		step, layout := labelStep(tt.interval)
		assert.Equal(t, tt.step, step, tt.interval.String())
		assert.Equal(t, tt.layout, layout, tt.interval.String())
	}
}

func TestNiceCeil(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want float64 }{
		{0, 1},
		{-3, 1},
		{math.NaN(), 1},
		{0.07, 0.08},
		{1, 1},
		{1.01, 1.2},
		{1.3, 1.5},
		{7, 8},
		{8.5, 10},
		{10, 10},
		{37, 40},
		{101, 120},
		{960, 1000},
	}
	for _, tt := range tests {
		assert.InDelta(t, tt.want, niceCeil(tt.in), 1e-9, "niceCeil(%v)", tt.in)
	}
}

func TestAxisTop(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, 1, axisTop(0, UnitCount, 0), 0, "a flat zero line keeps a minimum top")
	assert.InDelta(t, 100, axisTop(37, UnitPercent, 100), 0, "a ceiling is used as it is")
	assert.InDelta(t, 120, axisTop(101, UnitPercent, 100), 0, "data above the ceiling uses the ladder")
	assert.InDelta(t, 1<<30, axisTop(900<<20, UnitBytes, 0), 0, "a ladder top near 1000 MiB becomes 1 GiB")
	assert.InDelta(t, 2.5*(1<<20), axisTop(2.2*(1<<20), UnitBytes, 0), 1, "bytes use the ladder inside one prefix")
}

func TestLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		v     float64
		unit  Unit
		axis  string
		value string
	}{
		{0, UnitCount, "0", "0"},
		{0.5, UnitCount, "0.5", "0.5"},
		{12.5, UnitPercent, "12.5", "12.5"},
		{1250, UnitPerSecond, "1.25k", "1.25k"},
		{512, UnitBytes, "512", "512 B"},
		{1536, UnitBytes, "1.5Ki", "1.5 KiB"},
		{300 << 20, UnitBytes, "300Mi", "300 MiB"},
		{1.25 * (1 << 20), UnitBytes, "1.25M", "1.25 MiB"},
		{3 << 30, UnitBytesPerSecond, "3Gi", "3 GiB/s"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.axis, axisLabel(tt.v, tt.unit), "axisLabel(%v)", tt.v)
		assert.Equal(t, tt.value, FormatReading(tt.v, tt.unit), "FormatReading(%v)", tt.v)
		assert.LessOrEqual(t, len(axisLabel(tt.v, tt.unit)), axisLabelWidth)
	}
}

func TestAxisLabelsUseOneForm(t *testing.T) {
	t.Parallel()
	mib := float64(1 << 20)
	assert.Equal(t, []string{"2.5M", "1.25M", "0"}, axisLabels([]float64{2.5 * mib, 1.25 * mib, 0}, UnitBytes), "one label needs the short form, so all use it")
	assert.Equal(t, []string{"1Gi", "512Mi", "0"}, axisLabels([]float64{1 << 30, 512 * mib, 0}, UnitBytes), "full prefixes stay when they fit")
	assert.Equal(t, []string{"2.5k", "1.25k", "0"}, axisLabels([]float64{2500, 1250, 0}, UnitCount))
}

func TestSparkline(t *testing.T) {
	t.Parallel()
	require.Empty(t, Sparkline(nil, 0, 1))
	assert.Equal(t, "   ▁▂▃ █", Sparkline(series(step2s, 0, 1, 2, math.NaN(), 8), 8, 8), "a gap is blank and a zero is the lowest block")
	assert.Equal(t, "▆█", Sparkline(series(step2s, 1, 2, 3, 4), 2, 4), "only the newest points that fit are kept")
	assert.Equal(t, "▁▁", Sparkline(series(step2s, 1, 2), 2, 0), "no ceiling draws the lowest block")
	assert.Equal(t, "▁█", Sparkline(series(step2s, -3, 9), 2, 8), "values are clamped to the scale")
}
