// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/model/cmd"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/tui/graph"
	"unikraft.com/cli/internal/tui/testkit"
)

const (
	graphTestUUID  = "00000000-0000-4000-8000-000000000001"
	graphTestUUID2 = "00000000-0000-4000-8000-000000000002"
)

var graphT0 = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

// graphStep is the answer to one poll.
type graphStep struct {
	metrics graphMetrics
	err     error
	// omit answers without the instance.
	omit bool
	// readings answers with these metrics, by UUID, instead of with metrics.
	readings graphReading
}

// graphScript answers polls in order and repeats the last answer.
type graphScript struct {
	mu    sync.Mutex
	calls int
	steps []graphStep
}

func (s *graphScript) fetch(context.Context) (graphReading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	step := s.steps[min(s.calls, len(s.steps)-1)]
	s.calls++
	switch {
	case step.err != nil:
		return nil, step.err
	case step.omit:
		return graphReading{}, nil
	case step.readings != nil:
		return step.readings, nil
	}
	step.metrics.Uuid = graphTestUUID
	return graphReading{graphTestUUID: step.metrics}, nil
}

func (s *graphScript) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func testTarget(name, uuid string) graphTarget {
	return graphTarget{
		key:         multimetro.Key{Metro: "fra", UUID: uuid},
		name:        name,
		vcpus:       2,
		memoryBytes: 256 << 20,
	}
}

func newTestSession(t *testing.T, fetch graphFetch, names ...string) *graphSession {
	t.Helper()
	return newTestSessionOf(t, fetch, []graphTarget{testTarget("my-instance", graphTestUUID)}, names...)
}

func newTestSessionOf(t *testing.T, fetch graphFetch, targets []graphTarget, names ...string) *graphSession {
	t.Helper()
	fields, err := parseGraphFields(names)
	require.NoError(t, err)
	return newGraphSession(targets, fields, 2*time.Second, fetch, time.UTC)
}

func mem(mib uint64) graphStep { return graphStep{metrics: graphMetrics{RssBytes: mib << 20}} }

func TestGraphCPUPercentUsesVCPUs(t *testing.T) {
	t.Parallel()
	s := newTestSession(t, nil, "cpu")
	require.NoError(t, s.observe(graphT0, graphReading{graphTestUUID: {CpuTimeMs: 1000}}, nil))
	assert.Empty(t, s.series[0], "the first sample of a counter has no rate")

	// 2000 CPU ms in 2 s on 2 vCPUs is 50 %.
	require.NoError(t, s.observe(graphT0.Add(2*time.Second), graphReading{graphTestUUID: {CpuTimeMs: 3000}}, nil))
	require.Len(t, s.series[0], 1)
	assert.True(t, s.series[0][0].OK)
	assert.InDelta(t, 50, s.series[0][0].V, 1e-9)
}

func TestGraphRatesUseReceiveTimes(t *testing.T) {
	t.Parallel()
	s := newTestSession(t, nil, "rx", "handled")
	require.NoError(t, s.observe(graphT0, graphReading{graphTestUUID: {RxBytes: 1000, Ntotal: 10}}, nil))
	// The answer took 4 s to arrive: the rate is over 4 s, not over the interval.
	require.NoError(t, s.observe(graphT0.Add(4*time.Second), graphReading{graphTestUUID: {RxBytes: 5000, Ntotal: 18}}, nil))
	assert.InDelta(t, 1000, s.series[0][0].V, 1e-9)
	assert.InDelta(t, 2, s.series[1][0].V, 1e-9)
}

func TestGraphGaugesAreNotRates(t *testing.T) {
	t.Parallel()
	s := newTestSession(t, nil, "memory")
	_ = s.observe(graphT0, graphReading{graphTestUUID: {RssBytes: 100 << 20}}, nil)
	require.Len(t, s.series[0], 1, "a gauge has a value on the first sample")
	assert.InDelta(t, 100<<20, s.series[0][0].V, 0)
}

func TestGraphCounterResetIsAGap(t *testing.T) {
	t.Parallel()
	s := newTestSession(t, nil, "rx")
	_ = s.observe(graphT0, graphReading{graphTestUUID: {RxBytes: 9000}}, nil)
	_ = s.observe(graphT0.Add(2*time.Second), graphReading{graphTestUUID: {RxBytes: 100}}, nil)
	_ = s.observe(graphT0.Add(4*time.Second), graphReading{graphTestUUID: {RxBytes: 300}}, nil)
	require.Len(t, s.series[0], 2)
	assert.False(t, s.series[0][0].OK, "a counter that goes down is a gap and never a negative rate")
	assert.True(t, s.series[0][1].OK)
	assert.InDelta(t, 100, s.series[0][1].V, 1e-9, "the next rate starts from the new count")
}

func TestGraphFailedPollIsAGapNotZero(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		reading graphReading
		err     error
		cause   error
	}{
		"failed poll":      {err: errors.New("boom"), cause: errors.New("boom")},
		"missing instance": {reading: graphReading{}, cause: errors.New("my-instance: no answer from the platform")},
		"other instance":   {reading: graphReading{"someone-else": {}}, cause: errors.New("my-instance: no answer from the platform")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newTestSession(t, nil, "memory", "conns")
			_ = s.observe(graphT0, graphReading{graphTestUUID: {RssBytes: 5, Nconns: 3}}, nil)
			err := s.observe(graphT0.Add(2*time.Second), tt.reading, tt.err)
			require.EqualError(t, err, tt.cause.Error())
			for i := range s.series {
				require.Len(t, s.series[i], 2)
				assert.True(t, s.series[i][0].OK)
				assert.False(t, s.series[i][1].OK)
			}
			assert.Equal(t, err, s.lastErr)
		})
	}
}

func TestGraphKeepsAtMostTheRingSize(t *testing.T) {
	t.Parallel()
	s := newTestSession(t, nil, "memory")
	for i := range graphRingSize + 10 {
		_ = s.observe(graphT0.Add(time.Duration(i)*time.Second), graphReading{graphTestUUID: {RssBytes: uint64(i)}}, nil)
	}
	require.Len(t, s.series[0], graphRingSize)
	assert.InDelta(t, graphRingSize+9, s.series[0][graphRingSize-1].V, 0, "the newest points are kept")
}

func TestGraphValidate(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		cmd     InstancesGraphCmd
		wantErr string
	}{
		"default":         {cmd: InstancesGraphCmd{Interval: 2 * time.Second}},
		"minimum":         {cmd: InstancesGraphCmd{Interval: time.Second}},
		"short interval":  {cmd: InstancesGraphCmd{Interval: 999 * time.Millisecond}, wantErr: "--interval must be at least 1s, got 999ms"},
		"zero interval":   {cmd: InstancesGraphCmd{}, wantErr: "--interval must be at least 1s"},
		"negative count":  {cmd: InstancesGraphCmd{Interval: time.Second, Samples: new(-1)}, wantErr: "--samples must not be negative"},
		"unknown field":   {cmd: InstancesGraphCmd{Interval: time.Second, Field: []string{"disk"}}, wantErr: `unknown field "disk": valid fields are cpu, memory, rx, tx, conns, reqs, queued, handled`},
		"repeated field":  {cmd: InstancesGraphCmd{Interval: time.Second, Field: []string{"cpu", "cpu"}}, wantErr: `field "cpu" is given twice`},
		"each valid name": {cmd: InstancesGraphCmd{Interval: time.Second, Field: []string{"handled"}}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fields, err := tt.cmd.validate()
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Len(t, fields, 1)
		})
	}
}

func TestGraphValidateAcceptsSeveralFields(t *testing.T) {
	t.Parallel()
	c := InstancesGraphCmd{Interval: time.Second, Field: []string{"cpu", "tx", "memory"}}
	fields, err := c.validate()
	require.NoError(t, err)
	require.Len(t, fields, 3)
	assert.Equal(t, "cpu", fields[0].name)
	assert.Equal(t, "memory", fields[2].name)
}

func TestGraphDefaultFieldIsMemory(t *testing.T) {
	t.Parallel()
	fields, err := parseGraphFields(nil)
	require.NoError(t, err)
	require.Len(t, fields, 1)
	assert.Equal(t, "memory", fields[0].name)
}

func TestGraphAllFieldsRead(t *testing.T) {
	t.Parallel()
	m := graphMetrics{
		RssBytes: 1, CpuTimeMs: 2, RxBytes: 3, TxBytes: 4, Nconns: 5, Nreqs: 6, Nqueued: 7, Ntotal: 8,
	}
	want := map[string]uint64{"memory": 1, "cpu": 2, "rx": 3, "tx": 4, "conns": 5, "reqs": 6, "queued": 7, "handled": 8}
	require.Len(t, graphFields, len(want))
	for _, f := range graphFields {
		assert.Equal(t, want[f.name], f.read(&m), f.name)
	}
}

func runGraph(t *testing.T, c *InstancesGraphCmd, s *graphSession) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := c.run(t.Context(), config.Stdio{Stdout: &out, Stderr: &out}, s)
	return out.String(), err
}

func TestGraphWithoutTerminalPrintsOneFrame(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{mem(100), mem(120), {err: errors.New("boom")}, mem(140)}}
		s := newTestSession(t, script.fetch, "memory")
		c := &InstancesGraphCmd{Interval: 2 * time.Second, Samples: new(4)}

		out, err := runGraph(t, c, s)
		require.NoError(t, err)

		assert.Equal(t, 4, script.count(), "one poll for each sample")
		assert.Equal(t, ansi.Strip(out), out, "a frame without a terminal has no escape sequences")
		assert.Equal(t, 1, strings.Count(out, "my-instance  memory (bytes)"), "one frame")
		assert.Contains(t, out, "last 140 MiB")
		assert.Contains(t, out, "256Mi┤", "the memory limit tops the axis")
		assert.NotContains(t, out, "error:", "the last poll worked")
	})
}

func TestGraphFrameShowsTheLastError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{mem(100), {err: errors.New("bad\x1b[31m gateway\nretry")}}}
		s := newTestSession(t, script.fetch, "memory")
		out, err := runGraph(t, &InstancesGraphCmd{Interval: time.Second, Samples: new(2)}, s)
		require.NoError(t, err)
		assert.Contains(t, out, "\nerror: bad gateway retry")
		assert.NotContains(t, out, "\x1b")
	})
}

func TestGraphEveryPollFailingIsAnError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{{err: errors.New("unauthorized")}}}
		s := newTestSession(t, script.fetch, "memory")
		out, err := runGraph(t, &InstancesGraphCmd{Interval: time.Second, Samples: new(3)}, s)
		require.EqualError(t, err, "unauthorized")
		assert.Empty(t, out)
		assert.Equal(t, 3, script.count())
	})
}

func TestGraphCounterFrameWaitsForSecondSample(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{{metrics: graphMetrics{RxBytes: 10}}}}
		s := newTestSession(t, script.fetch, "rx")
		out, err := runGraph(t, &InstancesGraphCmd{Interval: time.Second, Samples: new(1)}, s)
		require.NoError(t, err)
		assert.Contains(t, out, "waiting for a second sample")
	})
}

func TestGraphInterruptPrintsWhatWasCollected(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		script := &graphScript{steps: []graphStep{mem(100)}}
		s := newTestSession(t, func(ctx context.Context) (graphReading, error) {
			reading, err := script.fetch(ctx)
			if script.count() == 3 {
				cancel()
			}
			return reading, err
		}, "memory")

		var out bytes.Buffer
		c := &InstancesGraphCmd{Interval: time.Second, Samples: new(0), Output: cmd.Printer{Type: cmd.PrinterTypeJSON}}
		require.NoError(t, c.run(ctx, config.Stdio{Stdout: &out, Stderr: &out}, s))

		var rows []map[string]any
		require.NoError(t, json.Unmarshal(out.Bytes(), &rows))
		assert.Len(t, rows, 2, "the poll that the interrupt cut short is dropped")
	})
}

func TestGraphOutputIsOneJSONDocument(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{
			mem(0), {omit: true}, {err: errors.New("boom")}, mem(7),
		}}
		s := newTestSession(t, script.fetch, "memory")
		c := &InstancesGraphCmd{Interval: time.Second, Samples: new(4), Output: cmd.Printer{Type: cmd.PrinterTypeJSON}}
		out, err := runGraph(t, c, s)
		require.NoError(t, err)

		var rows []struct {
			Time     time.Time `json:"time"`
			Instance string    `json:"instance"`
			Metric   string    `json:"metric"`
			Value    *float64  `json:"value"`
			Unit     string    `json:"unit"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &rows), "one valid document")
		require.Len(t, rows, 4)
		assert.InDelta(t, 0, *rows[0].Value, 0, "a real zero is a number")
		assert.Nil(t, rows[1].Value, "a missing instance is null")
		assert.Nil(t, rows[2].Value, "a failed poll is null")
		assert.InDelta(t, float64(7<<20), *rows[3].Value, 0)
		assert.Equal(t, "my-instance", rows[0].Instance)
		assert.Equal(t, "memory", rows[0].Metric)
		assert.Equal(t, "bytes", rows[0].Unit)
		assert.True(t, rows[1].Time.After(rows[0].Time))
	})
}

func TestGraphOutputTableMarksMissingValues(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{
			{metrics: graphMetrics{Nconns: 0}}, {err: errors.New("boom")}, {metrics: graphMetrics{Nconns: 2}},
		}}
		s := newTestSession(t, script.fetch, "conns")
		c := &InstancesGraphCmd{Interval: time.Second, Samples: new(3), Output: cmd.Printer{Type: cmd.PrinterTypeTable}}
		out, err := runGraph(t, c, s)
		require.NoError(t, err)

		lines := strings.Split(strings.TrimSpace(out), "\n")
		require.Len(t, lines, 4, "a header and one row for each sample")
		assert.Equal(t, []string{"TIME", "INSTANCE", "METRIC", "VALUE", "UNIT"}, strings.Fields(lines[0]))
		assert.Equal(t, []string{"2000-01-01T00:00:00Z", "my-instance", "conns", "0", "count"}, strings.Fields(lines[1]), "a real zero is 0")
		assert.Equal(t, []string{"2000-01-01T00:00:02Z", "my-instance", "conns", "-", "count"}, strings.Fields(lines[2]), "a failed poll is a dash")
		assert.Equal(t, "2", strings.Fields(lines[3])[3])
	})
}

func TestGraphOutputDefaultsToTenSamples(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{mem(1)}}
		s := newTestSession(t, script.fetch, "memory")
		c := &InstancesGraphCmd{Interval: time.Second, Output: cmd.Printer{Type: cmd.PrinterTypeQuiet}}
		_, err := runGraph(t, c, s)
		require.NoError(t, err)
		assert.Equal(t, defaultRowSamples, script.count())
	})
}

func TestGraphPlainFrameDefaultsToThirtySamples(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{mem(1)}}
		s := newTestSession(t, script.fetch, "memory")
		_, err := runGraph(t, &InstancesGraphCmd{Interval: time.Second}, s)
		require.NoError(t, err)
		assert.Equal(t, defaultPlainSamples, script.count())
	})
}

func TestCleanLine(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a b c", cleanLine("a\x1b[31m b\x1b]0;title\x07\r\n\tc"))
	assert.Empty(t, cleanLine("\x1b[2J\x00"))
}

func newTestModel(t *testing.T, script *graphScript, samples int) *graphModel {
	t.Helper()
	return &graphModel{ctx: t.Context(), session: newTestSession(t, script.fetch, "memory"), samples: samples}
}

func TestGraphModelResizeDoesNotFetch(t *testing.T) {
	t.Parallel()
	script := &graphScript{steps: []graphStep{mem(10)}}
	m := newTestModel(t, script, 0)
	_, cmd := m.Update(graphSampleMsg{at: graphT0, reading: graphReading{graphTestUUID: {RssBytes: 10 << 20}}})
	require.NotNil(t, cmd)

	wide := m.View().Content
	_, cmd = m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	assert.Nil(t, cmd, "a resize starts no request")
	assert.Equal(t, 0, script.count())
	narrow := m.View().Content
	assert.NotEqual(t, wide, narrow, "the chart follows the size")
	for line := range strings.SplitSeq(ansi.Strip(narrow), "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(line), 50)
	}
}

func TestGraphModelIgnoresTickWhileFetching(t *testing.T) {
	t.Parallel()
	script := &graphScript{steps: []graphStep{mem(10)}}
	m := newTestModel(t, script, 0)
	require.NotNil(t, m.Init())
	assert.True(t, m.fetching)

	_, cmd := m.Update(graphTickMsg{})
	assert.Nil(t, cmd, "one request at a time")

	m.Update(graphSampleMsg{at: graphT0, reading: graphReading{graphTestUUID: {}}})
	assert.False(t, m.fetching)
	_, cmd = m.Update(graphTickMsg{})
	require.NotNil(t, cmd)
	assert.True(t, m.fetching)
	cmd()
	assert.Equal(t, 1, script.count(), "the tick started the request")
}

func TestGraphModelKeepsDataOnError(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, &graphScript{steps: []graphStep{mem(10)}}, 0)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m.Update(graphSampleMsg{at: graphT0, reading: graphReading{graphTestUUID: {RssBytes: 64 << 20}}})
	_, cmd := m.Update(graphSampleMsg{at: graphT0.Add(2 * time.Second), err: errors.New("503 \x1b[2Junavailable")})
	require.NotNil(t, cmd)
	assert.NotEqual(t, tea.QuitMsg{}, runQuick(cmd), "a failed poll does not end the chart")

	view := ansi.Strip(m.View().Content)
	assert.Contains(t, view, "my-instance  memory (bytes)")
	assert.Contains(t, view, "error: 503 unavailable")
	assert.Len(t, m.session.series[0], 2)
	assert.True(t, m.session.series[0][0].OK)
	assert.False(t, m.session.series[0][1].OK)
}

// runQuick runs a command, unless it is a timer.
func runQuick(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

func TestGraphModelQuits(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, &graphScript{steps: []graphStep{mem(1)}}, 2)
	_, cmd := m.Update(graphSampleMsg{at: graphT0, reading: graphReading{graphTestUUID: {}}})
	assert.NotEqual(t, tea.QuitMsg{}, runQuick(cmd))
	_, cmd = m.Update(graphSampleMsg{at: graphT0.Add(time.Second), reading: graphReading{graphTestUUID: {}}})
	assert.Equal(t, tea.QuitMsg{}, cmd(), "the chart ends after the requested samples")

	m = newTestModel(t, &graphScript{steps: []graphStep{mem(1)}}, 0)
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	assert.Equal(t, tea.QuitMsg{}, cmd())
}

func TestGraphModelLive(t *testing.T) {
	t.Parallel()
	script := &graphScript{steps: []graphStep{mem(64), mem(128)}}
	m := newTestModel(t, script, 0)
	m.session.interval = 10 * time.Millisecond
	runner := testkit.New(m, 60, 20)
	defer runner.Stop()

	snap, err := runner.WaitUntilSnapshot(5*time.Second, 10*time.Millisecond, func(s string) bool {
		return strings.Contains(s, "last 128 MiB")
	})
	require.NoError(t, err)
	assert.Contains(t, snap, "256Mi┤")
	assert.GreaterOrEqual(t, script.count(), 2)
}

func TestGraphSampleFields(t *testing.T) {
	t.Parallel()
	sample := GraphSample{
		Time:     GraphTime(graphT0),
		Instance: "my-instance",
		Metric:   "cpu",
		Value:    GraphValue{v: new(12.5)},
		Unit:     "percent",
	}
	fields, err := sample.Fields(t.Context())
	require.NoError(t, err)
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.Name
	}
	assert.Equal(t, []string{"time", "instance", "metric", "value", "unit"}, names)
}

func TestGraphFrameUsesCeilings(t *testing.T) {
	t.Parallel()
	s := newTestSession(t, nil, "cpu")
	_ = s.observe(graphT0, graphReading{graphTestUUID: {CpuTimeMs: 0}}, nil)
	_ = s.observe(graphT0.Add(2*time.Second), graphReading{graphTestUUID: {CpuTimeMs: 400}}, nil)
	frame := s.frame(50, 0, graph.Style{})
	assert.Contains(t, frame, "my-instance  cpu (%)  last 10")
	assert.Contains(t, frame, "  100┤", "CPU is shown on a fixed 0 to 100 axis")
}
