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

	"charm.land/lipgloss/v2"
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

func TestChartFlatZero(t *testing.T) {
	t.Parallel()
	o := small()
	o.Title, o.Ceiling = "my-instance  conns", 0
	want := frame(`
my-instance  conns  last 0
    1┤
     │
  0.5┤
     │
    0┤                      ⣀⣀
           11:59         12:00
`)
	assert.Equal(t, want, Chart(series(step2s, 0, 0, 0, 0), o)+"\n")
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
	want := frame(`
t  last 8
    8┤                       ⠈
     │
    4┤
     │
    0┤                       ⡀
           11:59         12:00
`)
	assert.Equal(t, want, Chart(late, small())+"\n", "points more than 1.5 intervals apart are two dots")
	assert.NotEqual(t, Chart(series(step2s, 0, 8), small()), Chart(late, small()))
}

func TestChartSinglePointIsADot(t *testing.T) {
	t.Parallel()
	want := frame(`
t  last 4
    8┤
     │
    4┤                       ⠠
     │
    0┤
            11:59        12:00
`)
	assert.Equal(t, want, Chart(series(step2s, 4), small())+"\n")
}

func TestChartWithoutValuesShowsNote(t *testing.T) {
	t.Parallel()
	o := small()
	o.Empty = "waiting for a second sample"
	assert.Equal(t, "t  waiting for a second sample", strings.SplitN(Chart(nil, o), "\n", 2)[0])
	assert.Equal(t, "t  waiting for a second sample", strings.SplitN(Chart(series(step2s, math.NaN()), o), "\n", 2)[0])
}

func TestChartBytesUseIECLabels(t *testing.T) {
	t.Parallel()
	o := Options{Title: "m", Unit: UnitBytes, Width: 40, Height: 7, Interval: 30 * time.Second, Loc: time.UTC}
	want := frame(`
m  last 700 MiB
  1Gi┤                                 ⡄
     │                                ⢠⠻
512Mi┤                                ⡸
     │                               ⢀⠇
    0┤
               11:40     11:50     12:00
`)
	assert.Equal(t, want, Chart(series(30*time.Second, 200<<20, 300<<20, 600<<20, 900<<20, 700<<20), o)+"\n")
}

func TestChartPercentCeiling(t *testing.T) {
	t.Parallel()
	o := Options{Title: "my-instance  cpu (%)", Unit: UnitPercent, Ceiling: 100, Width: 36, Height: 9, Interval: step2s, Loc: time.UTC}
	want := frame(`
my-instance  cpu (%)  last 10
  100┤                           ⢸⡄
     │                           ⡎⢇
     │                          ⢸⠁⢸
   50┤                         ⢠⠇ ⠸⡀
     │                         ⡎   ⡇
     │                        ⢰⠁   ⢣
    0┤                        ⠁    ⠈
             11:59          12:00
`)
	assert.Equal(t, want, Chart(series(step2s, 10, 20, 30, 50, 50, 70, 70, 100, 90, 60, 30, 10), o)+"\n")

	o.Ceiling = 50
	got := Chart(series(step2s, 10, 20, 120), o)
	assert.Contains(t, got, "  120┤", "data above the ceiling moves the axis top to the ladder")
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

func TestChartKeepsOnlyNewestPointsThatFit(t *testing.T) {
	t.Parallel()
	o := small()
	o.Ceiling = 0
	values := make([]float64, 500)
	values[len(values)-1] = 5
	got := Chart(series(step2s, values...), o)
	assert.Contains(t, got, "t  last 5")
	for line := range strings.SplitSeq(got, "\n") {
		assert.LessOrEqual(t, len([]rune(line)), o.Width)
	}
}

func TestZeroStyleAddsNoEscapes(t *testing.T) {
	t.Parallel()
	assert.NotContains(t, Chart(series(step2s, 1, 3, 2), small()), "\x1b")
	assert.NotContains(t, Sparkline(series(step2s, 1, 3, 2), 8, 3), "\x1b")

	o := small()
	o.Style = Style{Line: lipgloss.NewStyle().Bold(true)}
	assert.Contains(t, Chart(series(step2s, 1, 3, 2), o), "\x1b", "a style is applied when set")
}

func TestChartIsDeterministic(t *testing.T) {
	t.Parallel()
	first := Chart(series(step2s, 1, 5, 3, math.NaN(), 7), small())
	second := Chart(series(step2s, 1, 5, 3, math.NaN(), 7), small())
	assert.Equal(t, first, second)
}

func TestTimeLabelsFollowTheZone(t *testing.T) {
	t.Parallel()
	o := small()
	o.Loc = time.FixedZone("plus-two", 2*60*60)
	lines := strings.Split(Chart(series(step2s, 1, 2, 3), o), "\n")
	assert.Contains(t, lines[len(lines)-1], "13:59")
	assert.NotContains(t, lines[len(lines)-1], "11:59")
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

func TestSparkline(t *testing.T) {
	t.Parallel()
	require.Empty(t, Sparkline(nil, 0, 1))
	assert.Equal(t, "   ▁▂▃ █", Sparkline(series(step2s, 0, 1, 2, math.NaN(), 8), 8, 8), "a gap is blank and a zero is the lowest block")
	assert.Equal(t, "▆█", Sparkline(series(step2s, 1, 2, 3, 4), 2, 4), "only the newest points that fit are kept")
	assert.Equal(t, "▁▁", Sparkline(series(step2s, 1, 2), 2, 0), "no ceiling draws the lowest block")
	assert.Equal(t, "▁█", Sparkline(series(step2s, -3, 9), 2, 8), "values are clamped to the scale")
}

func TestCeilingSharesOneScale(t *testing.T) {
	t.Parallel()
	a, b := series(step2s, 1, 2), series(step2s, 7, math.NaN())
	assert.InDelta(t, 7, Ceiling(a, b), 0)
	assert.InDelta(t, 0, Ceiling(), 0)
}
