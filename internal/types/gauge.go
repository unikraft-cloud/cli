// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"cmp"
	"encoding"
	"encoding/json"
	"fmt"

	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/tui/meter"
)

// gaugeThresholds only turn a gauge red when it's pinned at its maximum, as
// sitting high in the range is expected of a resource that scales.
var gaugeThresholds = meter.Thresholds{Warning: 0.75, Danger: 0.99}

// Gauge represents a current value that can move between a minimum and a
// maximum, rendered as a colored gauge. A gauge without a range (Max not
// above Min) renders as just its current value.
type Gauge[T numeric] struct {
	Current T
	Min     T
	Max     T
}

func (g Gauge[T]) hasRange() bool {
	return g.Max > g.Min
}

// Compare compares two gauges by their current values, returning -1, 0, or 1
// per the same contract as cmp.Compare.
func (g Gauge[T]) Compare(other Gauge[T]) int {
	return cmp.Compare(g.Current, other.Current)
}

func (g Gauge[T]) String() string {
	return fmt.Sprint(g.Current)
}

// Render implements value.Renderer, rendering the current value followed by
// a gauge and the range, e.g. "768MiB ┣━━━━━━╋───┤ 256MiB...1GiB". In short
// or quiet mode, or without a range, it renders just the current value.
func (g Gauge[T]) Render(opts value.RenderOpts) (string, error) {
	current, err := value.Render(g.Current, opts)
	if err != nil {
		return "", err
	}
	if opts.Quiet || opts.Short || !g.hasRange() {
		return current, nil
	}

	ratio := float64(g.Current-g.Min) / float64(g.Max-g.Min)
	gauge := meter.Gauge(ratio, meter.Width(), gaugeThresholds)

	// Right-pad the current value so gauges in neighbouring rows line up.
	return fmt.Sprintf("%-6s %s %v...%v", current, gauge, g.Min, g.Max), nil
}

func (g Gauge[T]) MarshalText() ([]byte, error) {
	return fmt.Append(nil, g.Current), nil
}

func (g Gauge[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(g.Current)
}

// UnmarshalText parses the current value, leaving the range unset.
func (g *Gauge[T]) UnmarshalText(text []byte) error {
	*g = Gauge[T]{}
	if u, ok := any(&g.Current).(encoding.TextUnmarshaler); ok {
		return u.UnmarshalText(text)
	}
	v, err := parseMeterUsagePart[T](string(text))
	if err != nil {
		return err
	}
	g.Current = v
	return nil
}
