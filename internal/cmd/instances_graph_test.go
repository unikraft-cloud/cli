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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/logfmt"
	"unikraft.com/cli/internal/model/cmd"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/tui/graph"
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
	return graphReading{graphTestUUID: {m: step.metrics}}, nil
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
	s := newTestSession(t, nil, "cpu", "rx", "handled")
	require.NoError(t, s.observe(graphT0, graphReading{graphTestUUID: {m: graphMetrics{CpuTimeMs: 1000, RxBytes: 1000, Ntotal: 10}}}, nil))
	for i := range s.series {
		require.Len(t, s.series[i], 1)
		assert.False(t, s.series[i][0].OK, "the first sample of a counter has no rate")
	}

	// The answer took 4 s to arrive: the rate is over 4 s, not over the interval.
	// 4000 CPU ms in 4 s on 2 vCPUs is 50 %.
	require.NoError(t, s.observe(graphT0.Add(4*time.Second), graphReading{graphTestUUID: {m: graphMetrics{CpuTimeMs: 5000, RxBytes: 5000, Ntotal: 18}}}, nil))
	assert.InDelta(t, 50, s.series[0][1].V, 1e-9)
	assert.InDelta(t, 1000, s.series[1][1].V, 1e-9)
	assert.InDelta(t, 2, s.series[2][1].V, 1e-9)
}

func TestGraphCounterResetIsAGap(t *testing.T) {
	t.Parallel()
	s := newTestSession(t, nil, "rx")
	_ = s.observe(graphT0, graphReading{graphTestUUID: {m: graphMetrics{RxBytes: 9000}}}, nil)
	_ = s.observe(graphT0.Add(2*time.Second), graphReading{graphTestUUID: {m: graphMetrics{RxBytes: 100}}}, nil)
	_ = s.observe(graphT0.Add(4*time.Second), graphReading{graphTestUUID: {m: graphMetrics{RxBytes: 300}}}, nil)
	require.Len(t, s.series[0], 3)
	assert.False(t, s.series[0][1].OK, "a counter that goes down is a gap and never a negative rate")
	assert.True(t, s.series[0][2].OK)
	assert.InDelta(t, 100, s.series[0][2].V, 1e-9, "the next rate starts from the new count")
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
		"other instance":   {reading: graphReading{"someone-else": {m: graphMetrics{}}}, cause: errors.New("my-instance: no answer from the platform")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newTestSession(t, nil, "memory", "conns")
			_ = s.observe(graphT0, graphReading{graphTestUUID: {m: graphMetrics{RssBytes: 5, Nconns: 3}}}, nil)
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
		_ = s.observe(graphT0.Add(time.Duration(i)*time.Second), graphReading{graphTestUUID: {m: graphMetrics{RssBytes: uint64(i)}}}, nil)
	}
	require.Len(t, s.series[0], graphRingSize)
	assert.InDelta(t, graphRingSize+9, s.series[0][graphRingSize-1].V, 0, "the newest points are kept")
}

func TestGraphValidate(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		cmd     InstancesGraphCmd
		wantErr string
		// want names the fields of a valid command. It is one field when empty.
		want []string
	}{
		"default":               {cmd: InstancesGraphCmd{Interval: 2 * time.Second}, want: []string{"memory"}},
		"several metrics":       {cmd: InstancesGraphCmd{Interval: time.Second, Metric: []string{"cpu", "tx", "memory"}}, want: []string{"cpu", "tx", "memory"}},
		"minimum":               {cmd: InstancesGraphCmd{Interval: time.Second}},
		"short interval":        {cmd: InstancesGraphCmd{Interval: 999 * time.Millisecond}, wantErr: "--interval must be at least 1s, got 999ms"},
		"zero interval":         {cmd: InstancesGraphCmd{}, wantErr: "--interval must be at least 1s"},
		"negative count":        {cmd: InstancesGraphCmd{Interval: time.Second, Samples: new(-1)}, wantErr: "--samples must not be negative"},
		"unknown metric":        {cmd: InstancesGraphCmd{Interval: time.Second, Metric: []string{"disk"}}, wantErr: `unknown metric "disk": valid metrics are cpu, memory, rx, tx, conns, reqs, queued, handled`},
		"repeated metric":       {cmd: InstancesGraphCmd{Interval: time.Second, Metric: []string{"cpu", "cpu"}}, wantErr: `metric "cpu" is given twice`},
		"each valid name":       {cmd: InstancesGraphCmd{Interval: time.Second, Metric: []string{"handled"}}},
		"one sample of a rate":  {cmd: InstancesGraphCmd{Interval: time.Second, Samples: new(1), Metric: []string{"cpu", "memory", "rx"}}, wantErr: "--samples must be 0 or at least 2 for cpu, rx: a rate needs two samples"},
		"one sample of a gauge": {cmd: InstancesGraphCmd{Interval: time.Second, Samples: new(1), Metric: []string{"memory"}}},
		"two samples of a rate": {cmd: InstancesGraphCmd{Interval: time.Second, Samples: new(2), Metric: []string{"cpu"}}},
		"until interrupted":     {cmd: InstancesGraphCmd{Interval: time.Second, Samples: new(0), Metric: []string{"cpu"}}},
		"quiet output":          {cmd: InstancesGraphCmd{Interval: time.Second, Output: cmd.Printer{Type: cmd.PrinterTypeQuiet}}, wantErr: "--output quiet is not supported here"},
		"debug output":          {cmd: InstancesGraphCmd{Interval: time.Second, Output: cmd.Printer{Type: cmd.PrintTypeDebug}}, wantErr: "--output debug is not supported here"},
		"json output":           {cmd: InstancesGraphCmd{Interval: time.Second, Output: cmd.Printer{Type: cmd.PrinterTypeJSON}}},
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
			if tt.want == nil {
				assert.Len(t, fields, 1)
				return
			}
			names := make([]string, len(fields))
			for i, f := range fields {
				names[i] = f.name
			}
			assert.Equal(t, tt.want, names)
		})
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

func TestGraphFrameKeepsTheLastErrorOffStdout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{mem(100), {err: errors.New("bad\x1b[31m gateway\nretry")}}}
		s := newTestSession(t, script.fetch, "memory")
		var stdout, stderr, logs bytes.Buffer
		ctx := log.WithLogger(t.Context(), logfmt.New(&logs, log.JSONType, log.InfoLevel))
		c := &InstancesGraphCmd{Interval: time.Second, Samples: new(2)}
		require.NoError(t, c.run(ctx, config.Stdio{Stdout: &stdout, Stderr: &stderr}, s))
		assert.NotContains(t, stdout.String(), "error:", "a piped chart stays a chart")
		assert.Empty(t, stderr.String(), "the error goes to the logger and not to stderr")

		var record map[string]any
		require.NoError(t, json.Unmarshal(logs.Bytes(), &record))
		assert.Equal(t, "warn", record["level"])
		assert.Equal(t, "the last poll failed: bad gateway retry", record["message"], "escape sequences are cleaned")
	})
}

func TestGraphTimeoutPrintsWhatWasCollectedAndFails(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		script := &graphScript{steps: []graphStep{mem(100)}}
		s := newTestSession(t, script.fetch, "memory")

		var out bytes.Buffer
		c := &InstancesGraphCmd{Interval: time.Second, Samples: new(0), Output: cmd.Printer{Type: cmd.PrinterTypeJSON}}
		err := c.run(ctx, config.Stdio{Stdout: &out, Stderr: &out}, s)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		var rows []map[string]any
		require.NoError(t, json.Unmarshal(out.Bytes(), &rows))
		assert.Len(t, rows, 3, "the samples before the deadline are printed: at 0 s, 2 s and 4 s")
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

func TestGraphWithoutAnyValueIsAnError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{{metrics: graphMetrics{RxBytes: 10}}}}
		for _, printer := range []cmd.Printer{{}, {Type: cmd.PrinterTypeJSON}} {
			s := newTestSession(t, script.fetch, "rx")
			out, err := runGraph(t, &InstancesGraphCmd{Interval: time.Second, Samples: new(1), Output: printer}, s)
			require.EqualError(t, err, "no values were collected", "one sample of a rate gives no value")
			assert.Empty(t, out)
		}
	})
}

func TestGraphInterruptBeforeAnyValueIsNotAnError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := newTestSession(t, nil, "rx")
	assert.NoError(t, s.collectFailure(ctx))
}

func TestGraphKeepsTheValuesOfAnsweredInstances(t *testing.T) {
	t.Parallel()
	s := newTestSessionOf(t, nil, twoTargets(), "conns")
	cause := errors.New("fra: 503")
	err := s.observe(graphT0, graphReading{graphTestUUID: {m: graphMetrics{Nconns: 4}}}, cause)
	require.ErrorIs(t, err, cause)
	assert.True(t, s.series[0][0].OK, "the instance that was answered keeps its value")
	assert.False(t, s.series[1][0].OK)
	assert.Equal(t, cause, s.lastErr)
}

func TestReadInstancesReportsTheMessageOfAnAnswer(t *testing.T) {
	t.Parallel()
	ok, failed := platform.ResponseStatusSuccess, platform.ResponseStatusError
	reading := graphReading{}
	at := graphT0.Add(time.Second)
	err := readInstances(reading, []graphMetrics{
		{Status: &ok, Uuid: graphTestUUID, Name: "web-1", RssBytes: 5},
		{Status: &failed, Uuid: graphTestUUID2, Name: "web-2", Message: new("instance is \x1b[31mnot running\n")},
		{Status: &failed, Uuid: "other", Name: "web-3"},
	}, at)
	assert.Len(t, reading, 1)
	assert.Equal(t, at, reading[graphTestUUID].at, "an answer keeps the time at which it arrived")
	assert.EqualError(t, err, "web-2: instance is not running\nweb-3: no answer from the platform")
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

func TestGraphSamplesDefault(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		output cmd.Printer
		want   int
	}{
		"rows":        {output: cmd.Printer{Type: cmd.PrinterTypeJSON}, want: defaultRowSamples},
		"plain frame": {want: defaultPlainSamples},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				script := &graphScript{steps: []graphStep{mem(1)}}
				s := newTestSession(t, script.fetch, "memory")
				_, err := runGraph(t, &InstancesGraphCmd{Interval: time.Second, Output: tt.output}, s)
				require.NoError(t, err)
				assert.Equal(t, tt.want, script.count())
			})
		})
	}
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

// runCmds runs a command, and the commands of a batch, and returns their
// messages. Run it inside a synctest bubble: a timer then ends when the
// fake clock reaches it.
func runCmds(cmd tea.Cmd) []tea.Msg {
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var msgs []tea.Msg
	for _, c := range batch {
		wg.Go(func() {
			got := runCmds(c)
			mu.Lock()
			defer mu.Unlock()
			msgs = append(msgs, got...)
		})
	}
	wg.Wait()
	return msgs
}

func TestGraphModelResizeDoesNotFetch(t *testing.T) {
	t.Parallel()
	script := &graphScript{steps: []graphStep{mem(10)}}
	m := newTestModel(t, script, 0)
	_, cmd := m.Update(graphSampleMsg{at: graphT0, reading: graphReading{graphTestUUID: {m: graphMetrics{RssBytes: 10 << 20}}}})
	assert.Nil(t, cmd, "an answer starts no timer: the ticks run on their own")

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

func TestGraphModelPollsOnAFixedSchedule(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		script := &graphScript{steps: []graphStep{mem(10)}}
		m := newTestModel(t, script, 0)
		start := time.Now()

		msgs := runCmds(m.Init())
		require.Len(t, msgs, 2, "the first request and the first tick")
		assert.Equal(t, 1, script.count())
		assert.True(t, m.fetching)
		assert.Contains(t, msgs, graphTickMsg{})

		// The first tick arrives while the request still runs: no second request.
		_, cmd := m.Update(graphTickMsg{})
		require.NotNil(t, cmd)
		assert.Equal(t, []tea.Msg{graphTickMsg{}}, runCmds(cmd), "the tick is armed again at once")
		assert.Equal(t, 1, script.count(), "one request at a time")

		m.Update(graphSampleMsg{at: time.Now(), reading: graphReading{graphTestUUID: {m: graphMetrics{}}}})
		assert.False(t, m.fetching)

		// The next tick starts a request and arms the next tick.
		_, cmd = m.Update(graphTickMsg{})
		require.NotNil(t, cmd)
		assert.True(t, m.fetching)
		msgs = runCmds(cmd)
		assert.Len(t, msgs, 2)
		assert.Equal(t, 2, script.count())
		assert.Equal(t, 3*m.session.interval, time.Since(start), "each tick is one interval after the one before it")
	})
}

func TestGraphModelKeepsDataOnError(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, &graphScript{steps: []graphStep{mem(10)}}, 0)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m.Update(graphSampleMsg{at: graphT0, reading: graphReading{graphTestUUID: {m: graphMetrics{RssBytes: 64 << 20}}}})
	_, cmd := m.Update(graphSampleMsg{at: graphT0.Add(2 * time.Second), err: errors.New("503 \x1b[2Junavailable")})
	assert.Nil(t, cmd, "a failed poll does not end the chart")

	view := ansi.Strip(m.View().Content)
	assert.Contains(t, view, "my-instance  memory (bytes)")
	assert.Contains(t, view, "error: 503 unavailable")
	assert.Len(t, m.session.series[0], 2)
	assert.True(t, m.session.series[0][0].OK)
	assert.False(t, m.session.series[0][1].OK)
}

func TestGraphModelQuits(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, &graphScript{steps: []graphStep{mem(1)}}, 2)
	_, cmd := m.Update(graphSampleMsg{at: graphT0, reading: graphReading{graphTestUUID: {m: graphMetrics{}}}})
	assert.Nil(t, cmd)
	_, cmd = m.Update(graphSampleMsg{at: graphT0.Add(time.Second), reading: graphReading{graphTestUUID: {m: graphMetrics{}}}})
	assert.Equal(t, tea.QuitMsg{}, cmd(), "the chart ends after the requested samples")

	m = newTestModel(t, &graphScript{steps: []graphStep{mem(1)}}, 0)
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	assert.Equal(t, tea.QuitMsg{}, cmd())
}

func TestGraphFrameUsesCeilings(t *testing.T) {
	t.Parallel()
	s := newTestSession(t, nil, "cpu")
	_ = s.observe(graphT0, graphReading{graphTestUUID: {m: graphMetrics{CpuTimeMs: 0}}}, nil)
	_ = s.observe(graphT0.Add(2*time.Second), graphReading{graphTestUUID: {m: graphMetrics{CpuTimeMs: 400}}}, nil)
	frame := s.frame(50, 0, graph.Style{})
	assert.Contains(t, frame, "my-instance  cpu (%)  last 10")
	assert.Contains(t, frame, "  100┤", "CPU is shown on a fixed 0 to 100 axis")
}

func TestGraphOutputKeepsEverySample(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		n := graphRingSize + 10
		steps := make([]graphStep, n)
		for i := range steps {
			steps[i] = mem(uint64(i))
		}
		script := &graphScript{steps: steps}
		s := newTestSession(t, script.fetch, "memory")
		c := &InstancesGraphCmd{Interval: time.Second, Samples: new(n), Output: cmd.Printer{Type: cmd.PrinterTypeJSON}}
		out, err := runGraph(t, c, s)
		require.NoError(t, err)

		var rows []struct {
			Time  time.Time `json:"time"`
			Value *float64  `json:"value"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &rows))
		require.Len(t, rows, n, "every sample that was polled is printed")
		assert.InDelta(t, 0, *rows[0].Value, 0, "the oldest sample is kept")
		assert.True(t, rows[0].Time.Equal(start), "the first row is at the first poll")
	})
}

func TestGraphRatesUseTheTimeOfEachAnswer(t *testing.T) {
	t.Parallel()
	s := newTestSessionOf(t, nil, twoTargets(), "rx")
	slow := 1500 * time.Millisecond

	// Poll 1: the first metro answers at once and the second after 1.5 s, so
	// the poll ends at 1.5 s. Poll 2 ends at 2 s, with both answers at once.
	// The first instance receives 1000 bytes each second.
	require.NoError(t, s.observe(graphT0.Add(slow), graphReading{
		graphTestUUID:  {m: graphMetrics{RxBytes: 0}, at: graphT0},
		graphTestUUID2: {m: graphMetrics{RxBytes: 0}, at: graphT0.Add(slow)},
	}, nil))
	require.NoError(t, s.observe(graphT0.Add(2*time.Second), graphReading{
		graphTestUUID:  {m: graphMetrics{RxBytes: 2000}, at: graphT0.Add(2 * time.Second)},
		graphTestUUID2: {m: graphMetrics{RxBytes: 500}, at: graphT0.Add(2 * time.Second)},
	}, nil))

	require.Len(t, s.series[0], 2)
	assert.InDelta(t, 1000, s.series[0][1].V, 1e-9, "the late metro does not skew the rate of the early one")
	assert.InDelta(t, 1000, s.series[1][1].V, 1e-9, "500 bytes in 0.5 s")
	assert.True(t, s.series[0][0].T.Equal(graphT0.Add(slow)), "the points stay at the time of the poll, so the series align")
	assert.True(t, s.series[0][1].T.Equal(graphT0.Add(2*time.Second)))
}

func TestGraphFetchReadsAPartialAnswer(t *testing.T) {
	t.Parallel()
	for name, code := range map[string]int{"item not found": 8, "other item error": 11} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"partial_success","message":"partial","data":{"instances":[` +
					`{"status":"success","uuid":"` + graphTestUUID + `","name":"web-1","rss_bytes":5000},` +
					`{"status":"error","uuid":"` + graphTestUUID2 + `","name":"web-2","error":` + strconv.Itoa(code) + `,"message":"metrics unavailable"}]}}`))
			}))
			t.Cleanup(srv.Close)

			g := group.New[multimetro.MetroClient]().WithClient("fra", multimetro.MetroClient{
				Client: platform.NewClient(platform.WithDefaultMetro(srv.URL)),
			})
			keys := multimetro.Keys{{Metro: "fra", UUID: graphTestUUID}, {Metro: "fra", UUID: graphTestUUID2}}
			reading, err := newGraphFetch(g, keys)(t.Context())

			require.Contains(t, reading, graphTestUUID, "the instance that the platform answered for keeps its metrics")
			assert.Equal(t, uint64(5000), reading[graphTestUUID].m.RssBytes)
			assert.False(t, reading[graphTestUUID].at.IsZero(), "the answer has the time at which it arrived")
			assert.NotContains(t, reading, graphTestUUID2)
			assert.EqualError(t, err, "web-2: metrics unavailable")
		})
	}
}

func TestGraphStatMatchesTheDrawnWindow(t *testing.T) {
	t.Parallel()
	s := newTestSessionOf(t, nil, twoTargets(), "conns")
	// An old spike of 1000 formats as 1k. It scrolls off the sparkline, and
	// the newest points, 999, are wider than the maximum of all points.
	for i := range 100 {
		v := uint64(999)
		if i == 0 {
			v = 1000
		}
		require.NoError(t, s.observe(graphT0.Add(time.Duration(i)*s.interval), graphReading{
			graphTestUUID:  {m: graphMetrics{Nconns: v}},
			graphTestUUID2: {m: graphMetrics{Nconns: v}},
		}, nil))
	}
	frame := s.plainFrame(60)
	lines := strings.Split(frame, "\n")
	require.Len(t, lines, 2)
	for _, line := range lines {
		assert.Contains(t, line, "last 999 / max 999", "the stat is not cut")
		assert.LessOrEqual(t, ansi.StringWidth(line), 60)
	}
}

func TestGraphModelKeepsEnoughPointsForAWideChart(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, &graphScript{steps: []graphStep{mem(1)}}, 0)
	m.Update(tea.WindowSizeMsg{Width: 400, Height: 20})
	for i := range 900 {
		m.Update(graphSampleMsg{at: graphT0.Add(time.Duration(i) * m.session.interval), reading: graphReading{graphTestUUID: {m: graphMetrics{RssBytes: 5 << 20}}}})
	}
	assert.Len(t, m.session.series[0], 800, "two points for each column")

	drawn := func(r rune) bool { return r > '\u2800' && r <= '\u28ff' }
	var plot string
	for line := range strings.SplitSeq(ansi.Strip(m.View().Content), "\n") {
		if _, rest, ok := strings.Cut(line, "┤"); ok && strings.IndexFunc(rest, drawn) >= 0 {
			plot = rest
			break
		}
	}
	require.NotEmpty(t, plot, "the chart has a plot")
	first, _ := utf8.DecodeRuneInString(plot)
	assert.True(t, drawn(first), "the leftmost plot cell is drawn")

	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m.Update(graphSampleMsg{at: graphT0.Add(time.Hour), reading: graphReading{graphTestUUID: {m: graphMetrics{RssBytes: 5 << 20}}}})
	assert.Len(t, m.session.series[0], graphRingSize, "a smaller window trims the series on the next point")
}
