// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/model/cmd"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/tui/graph"
)

func twoTargets() []graphTarget {
	return []graphTarget{testTarget("web-1", graphTestUUID), testTarget("web-2", graphTestUUID2)}
}

// observeMemory feeds a session one poll for each value. The second instance
// has half the memory and ten times the connections of the first.
func observeMemory(t *testing.T, s *graphSession, mibs ...uint64) {
	t.Helper()
	for i, mib := range mibs {
		at := graphT0.Add(time.Duration(i) * s.interval)
		reading := graphReading{
			graphTestUUID:  {RssBytes: mib << 20, Nconns: uint64(i)},
			graphTestUUID2: {RssBytes: mib << 19, Nconns: 10 * uint64(i)},
		}
		require.NoError(t, s.observe(at, reading, nil))
	}
}

func TestGraphSparkRowsShareOneScalePerField(t *testing.T) {
	t.Parallel()
	s := newTestSessionOf(t, nil, twoTargets(), "memory", "conns")
	observeMemory(t, s, 10, 20, 40, 30)
	want := strings.TrimPrefix(`
web-1 memory                  ▃▅█▆  last 30 MiB / max 40 MiB
web-2 memory                  ▂▃▅▄  last 15 MiB / max 20 MiB
web-1 conns                   ▁▁▁▂  last 3 / max 3
web-2 conns                   ▁▃▆█  last 30 / max 30
`, "\n")
	assert.Equal(t, want, s.plainFrame(60)+"\n", "rows of a field are next to each other and share a scale")
}

func TestGraphSparkRowsLeaveOutWhatIsTheSame(t *testing.T) {
	t.Parallel()
	byTarget := newTestSessionOf(t, nil, twoTargets(), "memory")
	observeMemory(t, byTarget, 10)
	assert.Contains(t, byTarget.plainFrame(60), "web-1  ")
	assert.NotContains(t, byTarget.plainFrame(60), "memory")

	byField := newTestSession(t, nil, "memory", "conns")
	require.NoError(t, byField.observe(graphT0, graphReading{graphTestUUID: {RssBytes: 1 << 20}}, nil))
	assert.Contains(t, byField.plainFrame(60), "memory  ")
	assert.NotContains(t, byField.plainFrame(60), "my-instance")
}

func TestGraphSparkRowsCapRows(t *testing.T) {
	t.Parallel()
	s := newTestSessionOf(t, nil, twoTargets(), "memory", "conns", "rx")
	observeMemory(t, s, 10, 20)
	require.Len(t, s.lines, 6)

	lines := strings.Split(s.frame(60, 4, graph.Style{}), "\n")
	require.Len(t, lines, 4, "the rows and the count of the rest fit the height")
	assert.Equal(t, "+3 more", lines[3])

	assert.Len(t, strings.Split(s.frame(60, 0, graph.Style{}), "\n"), 6, "no height shows every row up to the limit")
}

func TestGraphSparkRowsTooNarrow(t *testing.T) {
	t.Parallel()
	s := newTestSessionOf(t, nil, twoTargets(), "memory")
	observeMemory(t, s, 10)
	got := s.frame(minSparkFrame-1, 0, graph.Style{})
	assert.Contains(t, got, "too small")
	assert.NotContains(t, got, "\n")
}

func TestGraphSparkRowsShowAGapForAnInstanceWithoutAnswer(t *testing.T) {
	t.Parallel()
	s := newTestSessionOf(t, nil, twoTargets(), "memory")
	observeMemory(t, s, 10, 20)
	err := s.observe(graphT0.Add(4*time.Second), graphReading{graphTestUUID: {RssBytes: 30 << 20}}, nil)
	require.EqualError(t, err, "web-2: no answer from the platform")

	frame := s.plainFrame(60)
	assert.Contains(t, frame, "▃▆█  last 30 MiB", "the instance with an answer goes on")
	assert.Contains(t, frame, "last - / max 10 MiB", "the other has a gap and never a zero")
	assert.Contains(t, frame, "\nerror: web-2: no answer from the platform")
}

func TestGraphSeveralInstancesCostOneRequestForEachPoll(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		both := graphReading{graphTestUUID: {RssBytes: 1 << 20}, graphTestUUID2: {RssBytes: 2 << 20}}
		script := &graphScript{steps: []graphStep{{readings: both}}}
		s := newTestSessionOf(t, script.fetch, twoTargets(), "memory", "conns")
		c := &InstancesGraphCmd{Interval: time.Second, Samples: new(3)}

		out, err := runGraph(t, c, s)
		require.NoError(t, err)
		assert.Equal(t, 3, script.count(), "one request for each poll, whatever the instances and fields")
		assert.Equal(t, 4, strings.Count(out, "\n"), "four rows")
		assert.NotContains(t, out, "\x1b")
	})
}

func TestGraphOutputHasRowsForEveryInstanceAndField(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		both := graphReading{graphTestUUID: {RssBytes: 1 << 20, Nconns: 1}, graphTestUUID2: {RssBytes: 2 << 20, Nconns: 2}}
		script := &graphScript{steps: []graphStep{{readings: both}}}
		s := newTestSessionOf(t, script.fetch, twoTargets(), "memory", "conns")
		c := &InstancesGraphCmd{Interval: time.Second, Samples: new(2), Output: cmd.Printer{Type: cmd.PrinterTypeJSON}}

		out, err := runGraph(t, c, s)
		require.NoError(t, err)
		var rows []struct {
			Time     time.Time `json:"time"`
			Instance string    `json:"instance"`
			Metric   string    `json:"metric"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &rows))
		require.Len(t, rows, 8)
		for i := 1; i < len(rows); i++ {
			assert.False(t, rows[i].Time.Before(rows[i-1].Time), "oldest first")
		}
		seen := map[string]bool{}
		for _, row := range rows {
			seen[row.Instance+"/"+row.Metric] = true
		}
		assert.Len(t, seen, 4)
	})
}

func TestGraphModelSparklinesUseTheWholeHeight(t *testing.T) {
	t.Parallel()
	s := newTestSessionOf(t, nil, twoTargets(), "memory", "conns", "rx")
	observeMemory(t, s, 10, 20)
	m := &graphModel{ctx: t.Context(), session: s}
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	lines := strings.Split(m.View().Content, "\n")
	assert.Len(t, lines, 6+1, "six rows and the status line")

	m.Update(tea.WindowSizeMsg{Width: 70, Height: 5})
	lines = strings.Split(m.View().Content, "\n")
	assert.Len(t, lines, 4+1, "the rows that fit, the count of the rest and the status line")
}

func TestGraphSparkRowsFitTheWidth(t *testing.T) {
	t.Parallel()
	long := []graphTarget{
		testTarget("a-rather-long-instance-name-1", graphTestUUID),
		testTarget("a-rather-long-instance-name-2", graphTestUUID2),
	}
	tests := map[string][]string{
		"one field":      {"rx"},
		"two fields":     {"rx", "memory"},
		"three fields":   {"handled", "rx", "memory"},
		"all the fields": {"cpu", "memory", "rx", "tx", "conns", "reqs", "queued", "handled"},
	}
	for name, fields := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newTestSessionOf(t, nil, long, fields...)
			for i := range 12 {
				at := graphT0.Add(time.Duration(i) * s.interval)
				big := uint64(i+1) * 987654321
				reading := graphReading{
					graphTestUUID:  {RssBytes: big, RxBytes: big * 3, TxBytes: big, CpuTimeMs: big, Ntotal: big, Nconns: big, Nreqs: big, Nqueued: big},
					graphTestUUID2: {RssBytes: big / 7, RxBytes: big, TxBytes: big / 5, CpuTimeMs: big / 3, Ntotal: big / 9, Nconns: big / 3, Nreqs: big / 4, Nqueued: big / 5},
				}
				_ = s.observe(at, reading, nil)
			}
			for width := minSparkFrame; width <= 120; width++ {
				frame := s.frame(width, 0, graph.Style{})
				rows := strings.Split(frame, "\n")
				require.Len(t, rows, len(s.lines))
				for _, line := range rows {
					assert.LessOrEqual(t, ansi.StringWidth(line), width, "width %d: %q", width, line)
				}
				for r, line := range rows {
					metric := fields[r/len(long)]
					if len(fields) > 1 {
						assert.Contains(t, line, " "+metric+" ", "width %d: the metric stays in the label", width)
					}
				}
			}
		})
	}
}

func TestGraphSparkRowsKeepTheMetricInAShortLabel(t *testing.T) {
	t.Parallel()
	long := []graphTarget{
		testTarget("a-rather-long-instance-name-1", graphTestUUID),
		testTarget("a-rather-long-instance-name-2", graphTestUUID2),
	}
	s := newTestSessionOf(t, nil, long, "rx", "memory")
	observeMemory(t, s, 10, 20)
	lines := strings.Split(s.plainFrame(60), "\n")
	require.Len(t, lines, 4)
	labels := map[string]bool{}
	for _, line := range lines {
		labels[strings.Fields(line)[0]+" "+strings.Fields(line)[1]] = true
	}
	assert.Len(t, labels, 4, "each row has its own label")
	assert.Contains(t, lines[0], " rx ")
	assert.Contains(t, lines[2], " memory ")
}

func TestGraphSparkRowsScaleOverTheVisiblePoints(t *testing.T) {
	t.Parallel()
	s := newTestSessionOf(t, nil, twoTargets(), "conns")
	for i, v := range []uint64{1000, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24} {
		reading := graphReading{graphTestUUID: {Nconns: v}, graphTestUUID2: {Nconns: v}}
		require.NoError(t, s.observe(graphT0.Add(time.Duration(i)*s.interval), reading, nil))
	}
	frame := s.plainFrame(minSparkFrame)
	assert.Contains(t, frame, "█  ", "the newest value tops the scale because the spike is off screen")
	assert.NotContains(t, frame, "1k", "the spike is not in the max that the row shows")
}

func TestQualifyGraphNames(t *testing.T) {
	t.Parallel()
	targets := []graphTarget{
		{key: multimetro.Key{Metro: "fra", UUID: graphTestUUID}, name: "web"},
		{key: multimetro.Key{Metro: "was", UUID: graphTestUUID2}, name: "web"},
		{key: multimetro.Key{Metro: "fra", UUID: "00000000-0000-4000-8000-000000000003"}, name: "db"},
	}
	qualifyGraphNames(targets)
	assert.Equal(t, "web (fra)", targets[0].name)
	assert.Equal(t, "web (was)", targets[1].name)
	assert.Equal(t, "db", targets[2].name, "a name that is alone stays")
}
