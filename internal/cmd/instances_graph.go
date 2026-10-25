// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/guesstermwidth"
	xio "unikraft.com/x/io"
	"unikraft.com/x/kingkong"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/cmd"
	"unikraft.com/cli/internal/model/value"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/tui/graph"
)

const (
	// minGraphInterval is the shortest time between two samples.
	minGraphInterval = time.Second
	// graphRingSize is how many samples of a series are kept for the chart.
	graphRingSize = 512

	// Samples taken when --samples is not given.
	defaultPlainSamples = 30
	defaultRowSamples   = 10
)

// InstancesGraphCmd graphs the live metrics of one or more instances.
//
// The platform reports the current value of each metric and keeps no history,
// so the series is built here by polling. Rates use the time at which each
// answer arrives.
type InstancesGraphCmd struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Instances to graph."`

	Field    []string      `short:"f" sep:"none" placeholder:"metric" help:"Metric to graph, given once for each metric. One of: cpu, memory, rx, tx, conns, reqs, queued, handled. Defaults to memory."`
	Interval time.Duration `short:"i" default:"2s" placeholder:"duration" help:"Time between samples. At least 1s."`
	Samples  *int          `short:"n" placeholder:"count" help:"Stop after this many samples. 0 runs until interrupted. Defaults to 0 on a terminal, 30 without one, and 10 with --output."`
	Output   cmd.Printer   `short:"o" placeholder:"format" help:"Print the samples as rows instead of drawing a chart. One of: kv, table, json, yaml, raw, quiet, template."`
}

func (c InstancesGraphCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Graph the memory use of an instance",
			Commands:    []string{"unikraft instances graph my-instance"},
		},
		{
			Description: "Graph CPU use, sampled every 5 seconds",
			Commands:    []string{"unikraft instances graph my-instance -f cpu -i 5s"},
		},
		{
			Description: "Compare the memory and CPU use of two instances as sparklines",
			Commands:    []string{"unikraft instances graph web-1 web-2 -f memory -f cpu"},
		},
		{
			Description: "Print 10 samples of the received bytes per second as JSON",
			Commands:    []string{"unikraft instances graph my-instance -f rx -n 10 -o json"},
		},
	}
}

func (c *InstancesGraphCmd) Run(ctx context.Context, stdio config.Stdio) error {
	fields, err := c.validate()
	if err != nil {
		return err
	}

	found, err := Instance{}.Get(ctx, c.Targets)
	if err != nil {
		return err
	}
	var targets []graphTarget
	for _, res := range found {
		instance, ok := res.(Instance)
		if !ok {
			return fmt.Errorf("unexpected resource %T", res)
		}
		target := graphTarget{
			key:         multimetro.Key{Metro: string(instance.Metro), UUID: instance.UUID},
			name:        cmp.Or(instance.Name, instance.UUID),
			vcpus:       max(instance.Resources.VCPUs, 1),
			memoryBytes: float64(instance.Resources.Memory) * (1 << 20),
		}
		if slices.ContainsFunc(targets, func(t graphTarget) bool { return t.key.UUID == target.key.UUID }) {
			continue
		}
		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return errors.New("no instances to graph")
	}
	qualifyGraphNames(targets)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	keys := make(multimetro.Keys, len(targets))
	for i, t := range targets {
		keys[i] = t.key
	}
	session := newGraphSession(targets, fields, c.Interval, newGraphFetch(g, keys), time.Local)
	return c.run(ctx, stdio, session)
}

// qualifyGraphNames adds the metro to the name of each instance that shares
// its name with another instance, so that rows and charts stay apart.
func qualifyGraphNames(targets []graphTarget) {
	count := map[string]int{}
	for _, t := range targets {
		count[t.name]++
	}
	for i, t := range targets {
		if count[t.name] > 1 {
			targets[i].name = t.name + " (" + t.key.Metro + ")"
		}
	}
}

// validate refuses bad flag values and returns the fields to graph.
func (c *InstancesGraphCmd) validate() ([]graphField, error) {
	if c.Interval < minGraphInterval {
		return nil, fmt.Errorf("--interval must be at least %s, got %s", minGraphInterval, c.Interval)
	}
	if c.Samples != nil && *c.Samples < 0 {
		return nil, fmt.Errorf("--samples must not be negative, got %d", *c.Samples)
	}
	fields, err := parseGraphFields(c.Field)
	if err != nil {
		return nil, err
	}
	return fields, nil
}

// samplesOr returns --samples, or def when the flag is not set.
func (c *InstancesGraphCmd) samplesOr(def int) int {
	if c.Samples == nil {
		return def
	}
	return *c.Samples
}

// run picks the output mode. The live chart needs a terminal. Every other mode
// collects its samples first and prints once.
func (c *InstancesGraphCmd) run(ctx context.Context, stdio config.Stdio, s *graphSession) error {
	if c.Output.Type != "" {
		s.collect(ctx, c.samplesOr(defaultRowSamples))
		if err := s.failure(); err != nil {
			return err
		}
		return c.Output.Print(context.WithoutCancel(ctx), stdio.Stdout, nil, GraphSample{}, s.rows()...)
	}
	if xio.IsTTY(stdio.Stdout) {
		return runGraphLive(ctx, stdio, s, c.samplesOr(0))
	}
	s.collect(ctx, c.samplesOr(defaultPlainSamples))
	if err := s.failure(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stdio.Stdout, s.plainFrame(guesstermwidth.GuessTermWidth(stdio.Stdout)))
	return err
}

// graphTarget is the instance that is graphed.
type graphTarget struct {
	key         multimetro.Key
	name        string
	vcpus       int
	memoryBytes float64
}

// graphMetrics is one answer of the metrics endpoint.
type graphMetrics = platform.GetInstancesMetricsResponseInstanceMetrics

// graphReading holds the metrics of the instances that the platform answered
// for, by UUID.
type graphReading map[string]graphMetrics

// graphFetch reads the current metrics once.
type graphFetch func(ctx context.Context) (graphReading, error)

// graphField is a metric that can be graphed.
type graphField struct {
	name  string
	title string
	// unit is the unit of the rows that --output prints.
	unit string
	// kind picks how the chart scales and labels values.
	kind graph.Unit
	// counter marks a value that only grows. It is graphed as a rate.
	counter bool
	read    func(m *graphMetrics) uint64
	// suffix follows the values in sparkline rows.
	suffix string
	// scale turns a rate per second into the graphed value. Nil means 1.
	scale func(t graphTarget) float64
	// ceiling fixes the top of the chart axis. Nil means none.
	ceiling func(t graphTarget) float64
}

var graphFields = []graphField{
	{
		name: "cpu", title: "cpu (%)", unit: "percent", kind: graph.UnitPercent, counter: true, suffix: "%",
		read: func(m *graphMetrics) uint64 { return m.CpuTimeMs },
		// A rate of CPU milliseconds per second is a tenth of a percent of one vCPU.
		scale:   func(t graphTarget) float64 { return 1 / (10 * float64(max(t.vcpus, 1))) },
		ceiling: func(graphTarget) float64 { return 100 },
	},
	{
		name: "memory", title: "memory (bytes)", unit: "bytes", kind: graph.UnitBytes,
		read:    func(m *graphMetrics) uint64 { return m.RssBytes },
		ceiling: func(t graphTarget) float64 { return t.memoryBytes },
	},
	{
		name: "rx", title: "received (bytes/s)", unit: "bytes/s", kind: graph.UnitBytesPerSecond, counter: true,
		read: func(m *graphMetrics) uint64 { return m.RxBytes },
	},
	{
		name: "tx", title: "transmitted (bytes/s)", unit: "bytes/s", kind: graph.UnitBytesPerSecond, counter: true,
		read: func(m *graphMetrics) uint64 { return m.TxBytes },
	},
	{
		name: "conns", title: "open connections", unit: "count", kind: graph.UnitCount,
		read: func(m *graphMetrics) uint64 { return m.Nconns },
	},
	{
		name: "reqs", title: "in-flight requests", unit: "count", kind: graph.UnitCount,
		read: func(m *graphMetrics) uint64 { return m.Nreqs },
	},
	{
		name: "queued", title: "queued requests", unit: "count", kind: graph.UnitCount,
		read: func(m *graphMetrics) uint64 { return m.Nqueued },
	},
	{
		name: "handled", title: "handled (per second)", unit: "per second", kind: graph.UnitPerSecond, counter: true, suffix: "/s",
		read: func(m *graphMetrics) uint64 { return m.Ntotal },
	},
}

// parseGraphFields maps the names of --field to fields. No name means memory.
func parseGraphFields(names []string) ([]graphField, error) {
	if len(names) == 0 {
		names = []string{"memory"}
	}
	var fields []graphField
	for _, name := range names {
		i := slices.IndexFunc(graphFields, func(f graphField) bool { return f.name == name })
		if i < 0 {
			valid := make([]string, len(graphFields))
			for j, f := range graphFields {
				valid[j] = f.name
			}
			return nil, fmt.Errorf("unknown field %q: valid fields are %s", name, strings.Join(valid, ", "))
		}
		if slices.ContainsFunc(fields, func(f graphField) bool { return f.name == name }) {
			return nil, fmt.Errorf("field %q is given twice", name)
		}
		fields = append(fields, graphFields[i])
	}
	return fields, nil
}

// newGraphFetch reads the metrics of the instances with one grouped request.
func newGraphFetch(g *group.Group[multimetro.MetroClient], keys multimetro.Keys) graphFetch {
	return func(ctx context.Context) (graphReading, error) {
		var mu sync.Mutex
		reading := graphReading{}
		err := group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
			resp, err := c.GetInstanceMetrics(ctx, refs.NameOrUUIDs(), platform.GetInstanceMetricsOpts{})
			if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, err
			}
			if resp == nil || resp.Data == nil {
				return refs, nil
			}
			mu.Lock()
			defer mu.Unlock()
			for _, m := range resp.Data.Instances {
				if m.Status == nil || *m.Status != platform.ResponseStatusSuccess {
					continue
				}
				reading[m.Uuid] = m
			}
			return refs, nil
		})
		return reading, err
	}
}

// errGraphMissing is the cause of a gap when the answer lacks an instance.
var errGraphMissing = errors.New("no answer from the platform")

// rawSample is the last value that was read of a counter.
type rawSample struct {
	t   time.Time
	v   uint64
	set bool
}

// graphLine is the series of one field of one instance.
type graphLine struct {
	target, field int
}

// graphSession collects the samples of the instances.
type graphSession struct {
	targets  []graphTarget
	fields   []graphField
	interval time.Duration
	fetch    graphFetch
	loc      *time.Location

	// lines, series and prev have one entry for each field of each
	// instance. The fields of an instance are together.
	lines  []graphLine
	series [][]graph.Point
	prev   []rawSample
	// limit bounds the points of a series. Zero means no bound.
	limit   int
	lastErr error
}

func newGraphSession(targets []graphTarget, fields []graphField, interval time.Duration, fetch graphFetch, loc *time.Location) *graphSession {
	var lines []graphLine
	for ti := range targets {
		for fi := range fields {
			lines = append(lines, graphLine{target: ti, field: fi})
		}
	}
	return &graphSession{
		targets:  targets,
		fields:   fields,
		interval: interval,
		fetch:    fetch,
		loc:      loc,
		lines:    lines,
		series:   make([][]graph.Point, len(lines)),
		prev:     make([]rawSample, len(lines)),
		limit:    graphRingSize,
	}
}

// observe records one poll that finished at t. It returns why the poll gave
// no values, or nil. A failed poll adds a gap to each series and never a zero.
func (s *graphSession) observe(t time.Time, reading graphReading, fetchErr error) error {
	cause := fetchErr
	for i, line := range s.lines {
		target, f := s.targets[line.target], s.fields[line.field]
		m, found := reading[target.key.UUID]
		if fetchErr != nil || !found {
			if cause == nil {
				cause = fmt.Errorf("%s: %w", target.name, errGraphMissing)
			}
			s.add(i, graph.Point{T: t})
			continue
		}
		raw := f.read(&m)
		if !f.counter {
			s.add(i, graph.Point{T: t, V: float64(raw), OK: true})
			continue
		}
		prev := s.prev[i]
		s.prev[i] = rawSample{t: t, v: raw, set: true}
		if !prev.set {
			continue
		}
		elapsed := t.Sub(prev.t).Seconds()
		if raw < prev.v || elapsed <= 0 {
			s.add(i, graph.Point{T: t})
			continue
		}
		rate := float64(raw-prev.v) / elapsed
		if f.scale != nil {
			rate *= f.scale(target)
		}
		s.add(i, graph.Point{T: t, V: rate, OK: true})
	}
	s.lastErr = cause
	return cause
}

func (s *graphSession) add(i int, p graph.Point) {
	s.series[i] = append(s.series[i], p)
	if s.limit > 0 && len(s.series[i]) > s.limit {
		s.series[i] = slices.Delete(s.series[i], 0, len(s.series[i])-s.limit)
	}
}

// hasValue reports whether any poll gave a value.
func (s *graphSession) hasValue() bool {
	for _, points := range s.series {
		if slices.ContainsFunc(points, func(p graph.Point) bool { return p.OK }) {
			return true
		}
	}
	return false
}

// failure returns the last error when no poll gave a value.
func (s *graphSession) failure() error {
	if s.hasValue() {
		return nil
	}
	return s.lastErr
}

// collect polls n times, one poll for each interval, or until ctx ends when n
// is zero. The first poll is at once. A poll that ctx cut short is dropped.
func (s *graphSession) collect(ctx context.Context, n int) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for i := 0; n == 0 || i < n; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
		reading, err := s.fetch(ctx)
		if ctx.Err() != nil {
			return
		}
		_ = s.observe(time.Now(), reading, err)
	}
}

const (
	// maxSparkRows is how many rows the sparkline view shows when the
	// terminal does not limit it.
	maxSparkRows = 20
	// maxSparkLabel is the widest label of a sparkline row.
	maxSparkLabel = 24
	// minSparkInstance is the fewest cells of an instance name in a label.
	minSparkInstance = 4
	// minSparkWidth is the narrowest sparkline.
	minSparkWidth = 8
	// minSparkFrame is the narrowest frame of the sparkline view.
	minSparkFrame = 40
	// sparkGap is the space between the columns of a row.
	sparkGap = 2
)

// statMode is how much a row says after its sparkline.
type statMode int

const (
	// statFull is the newest and the largest value.
	statFull statMode = iota
	// statLast is the newest value.
	statLast
	// statNone is nothing.
	statNone
)

// frame draws the session in a frame of the given size: a chart for one
// series, or one sparkline row for each series.
func (s *graphSession) frame(width, height int, style graph.Style) string {
	if len(s.lines) == 1 {
		return s.chart(0, width, height, style)
	}
	return s.sparkRows(width, height, style)
}

// plainFrame is a frame without styles, with the error of the last poll below.
func (s *graphSession) plainFrame(width int) string {
	frame := s.frame(width, 0, graph.Style{})
	if s.lastErr != nil {
		frame += "\nerror: " + cleanLine(s.lastErr.Error())
	}
	return frame
}

// chart draws one series as a line chart.
func (s *graphSession) chart(i, width, height int, style graph.Style) string {
	target, f := s.targets[s.lines[i].target], s.fields[s.lines[i].field]
	o := graph.Options{
		Title:    target.name + "  " + f.title,
		Unit:     f.kind,
		Width:    width,
		Height:   height,
		Interval: s.interval,
		Loc:      s.loc,
		Empty:    "waiting for the first sample",
		Style:    style,
	}
	if f.counter {
		o.Empty = "waiting for a second sample"
	}
	if f.ceiling != nil {
		o.Ceiling = f.ceiling(target)
	}
	return graph.Chart(s.series[i], o)
}

// sparkRow is a series in the sparkline view.
type sparkRow struct {
	// instance and metric are the parts of the label. A part that is the
	// same in every row is empty.
	instance, metric string
}

// width is the width of the label of the row.
func (r sparkRow) width() int {
	w := ansi.StringWidth(r.instance) + ansi.StringWidth(r.metric)
	if r.instance != "" && r.metric != "" {
		w++
	}
	return w
}

// label shortens the instance to fit in w cells. The metric always stays.
func (r sparkRow) label(w int) string {
	if r.instance == "" {
		return r.metric
	}
	if r.metric == "" {
		return shortenMiddle(r.instance, w)
	}
	return shortenMiddle(r.instance, w-1-ansi.StringWidth(r.metric)) + " " + r.metric
}

// shortenMiddle cuts the middle of s to fit in w cells. Names of one group
// often differ at the end, and the end stays.
func shortenMiddle(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	if w < 1 {
		return ""
	}
	tail := (w - 1) / 2
	head := w - 1 - tail
	return ansi.Truncate(s, head, "") + "…" + ansi.TruncateLeft(s, ansi.StringWidth(s)-tail, "")
}

// sparkRows draws each series as a row. Rows of one field share a scale, and
// are next to each other. Rows that do not fit are counted in the last line.
// No row is wider than width. When the width is short, the labels give up
// cells first, then the rows say less after the sparkline.
func (s *graphSession) sparkRows(width, height int, style graph.Style) string {
	if width < minSparkFrame {
		return style.Note.Render(ansi.Truncate(fmt.Sprintf("too small for sparklines (needs %d columns)", minSparkFrame), max(width, 1), ""))
	}

	order := make([]int, len(s.lines))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Or(cmp.Compare(s.lines[a].field, s.lines[b].field), cmp.Compare(s.lines[a].target, s.lines[b].target))
	})
	budget := maxSparkRows
	if height > 0 {
		budget = max(height, 2)
	}
	hidden := 0
	if len(order) > budget {
		hidden = len(order) - (budget - 1)
		order = order[:budget-1]
	}

	rows := make([]sparkRow, len(order))
	labelW, minLabelW := 0, 0
	for r, i := range order {
		rows[r] = s.sparkRow(i)
		labelW = max(labelW, rows[r].width())
		w := ansi.StringWidth(rows[r].metric)
		if rows[r].instance != "" {
			w += minSparkInstance
			if rows[r].metric != "" {
				w++
			}
		}
		minLabelW = max(minLabelW, w)
	}
	labelW = min(labelW, maxSparkLabel)

	// Pick the longest stat text that leaves a label and a sparkline.
	mode, statW := statFull, 0
	for ; mode < statNone; mode++ {
		statW = 0
		for _, i := range order {
			statW = max(statW, ansi.StringWidth(s.stat(i, mode, graphRingSize)))
		}
		if width-minSparkWidth-2*sparkGap-statW >= minLabelW {
			break
		}
	}
	if mode == statNone {
		statW = 0
	}
	room := width - minSparkWidth - sparkGap
	if statW > 0 {
		room -= sparkGap + statW
	}
	labelW = max(min(labelW, room), minLabelW)
	sparkW := width - labelW - sparkGap
	if statW > 0 {
		sparkW -= sparkGap + statW
	}

	// The scale of a field covers the points that are on screen.
	scale := make([]float64, len(s.fields))
	for fi := range s.fields {
		var group [][]graph.Point
		for i, line := range s.lines {
			if line.field == fi {
				group = append(group, lastPoints(s.series[i], sparkW))
			}
		}
		scale[fi] = graph.Ceiling(group...)
	}

	out := make([]string, 0, len(order)+1)
	for r, i := range order {
		label := rows[r].label(labelW)
		pad := strings.Repeat(" ", labelW-ansi.StringWidth(label))
		spark := graph.Sparkline(s.series[i], sparkW, scale[s.lines[i].field])
		row := style.Axis.Render(label+pad) + strings.Repeat(" ", sparkGap) + style.Line.Render(spark)
		if statW > 0 {
			stat := ansi.Truncate(s.stat(i, mode, sparkW), statW, "")
			row += strings.Repeat(" ", sparkGap) + style.Note.Render(stat)
		}
		out = append(out, row)
	}
	if hidden > 0 {
		out = append(out, style.Note.Render(fmt.Sprintf("+%d more", hidden)))
	}
	return strings.Join(out, "\n")
}

// sparkRow returns the label parts of a series. It leaves out what is the
// same in every row.
func (s *graphSession) sparkRow(i int) sparkRow {
	line := s.lines[i]
	row := sparkRow{instance: s.targets[line.target].name, metric: s.fields[line.field].name}
	switch {
	case len(s.targets) == 1:
		row.instance = ""
	case len(s.fields) == 1:
		row.metric = ""
	}
	return row
}

// lastPoints returns the newest n points.
func lastPoints(points []graph.Point, n int) []graph.Point {
	return points[max(0, len(points)-n):]
}

// stat is the text at the end of a sparkline row. It covers the newest
// window points, which are the points that the sparkline shows.
func (s *graphSession) stat(i int, mode statMode, window int) string {
	if mode == statNone {
		return ""
	}
	f := s.fields[s.lines[i].field]
	points := lastPoints(s.series[i], window)
	if !slices.ContainsFunc(points, func(p graph.Point) bool { return p.OK }) {
		return "no value"
	}
	last := "-"
	if p := points[len(points)-1]; p.OK {
		last = graph.FormatReading(p.V, f.kind) + f.suffix
	}
	if mode == statLast {
		return "last " + last
	}
	return "last " + last + " / max " + graph.FormatReading(graph.Ceiling(points), f.kind) + f.suffix
}

// cleanLine removes escape sequences and control characters from text that
// comes from a server, so that it cannot change the terminal.
func cleanLine(s string) string {
	return strings.Join(strings.FieldsFunc(ansi.Strip(s), unicode.IsControl), " ")
}

// rows returns the samples as resources, oldest first.
func (s *graphSession) rows() []model.Model {
	var rows []GraphSample
	for i, line := range s.lines {
		target, f := s.targets[line.target], s.fields[line.field]
		for _, p := range s.series[i] {
			row := GraphSample{
				Time:     GraphTime(p.T.In(s.loc)),
				Instance: target.name,
				Metric:   f.name,
				Unit:     f.unit,
				key:      target.key,
			}
			if p.OK {
				row.Value = NewGraphValue(p.V)
			}
			rows = append(rows, row)
		}
	}
	slices.SortStableFunc(rows, func(a, b GraphSample) int { return time.Time(a.Time).Compare(time.Time(b.Time)) })
	out := make([]model.Model, len(rows))
	for i, row := range rows {
		out[i] = row
	}
	return out
}

// GraphSample is one sample of one metric of an instance. It is a row of
// the output of "instances graph --output".
type GraphSample struct {
	Time     GraphTime  `field:"time,short" json:"time"`
	Instance string     `field:",short" json:"instance"`
	Metric   string     `field:",short" json:"metric"`
	Value    GraphValue `field:",short" json:"value"`
	Unit     string     `field:",short" json:"unit"`

	key multimetro.Key
}

func (GraphSample) Type() model.Type {
	return model.Type{
		Name:  "instance-graph-sample",
		Names: "instance-graph-samples",
	}
}

func (s GraphSample) Key() model.Key {
	return s.key
}

func (s GraphSample) Raw() any {
	return s
}

func (s GraphSample) Fields(ctx context.Context) ([]model.Field, error) {
	return model.FieldsFromStruct(s)
}

// GraphTime is the time at which a sample was received.
type GraphTime time.Time

func (t GraphTime) MarshalText() ([]byte, error) {
	return []byte(time.Time(t).Format(time.RFC3339Nano)), nil
}

func (t GraphTime) Render(value.RenderOpts) (string, error) {
	return time.Time(t).Format(time.RFC3339), nil
}

// GraphValue is the value of a sample. A sample without a value is null in
// JSON and "-" in a table, and a zero is a real zero.
type GraphValue struct {
	v *float64
}

// NewGraphValue returns the value of a sample that has a number.
func NewGraphValue(v float64) GraphValue {
	return GraphValue{v: &v}
}

func (v GraphValue) MarshalJSON() ([]byte, error) {
	if v.v == nil {
		return []byte("null"), nil
	}
	return strconv.AppendFloat(nil, *v.v, 'g', -1, 64), nil
}

func (v GraphValue) Render(value.RenderOpts) (string, error) {
	if v.v == nil {
		return "-", nil
	}
	return strconv.FormatFloat(math.Round(*v.v*1000)/1000, 'f', -1, 64), nil
}
