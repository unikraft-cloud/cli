// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"cmp"
	"fmt"
	"math"
	"strconv"
	"strings"

	"unikraft.com/cli/internal/model/value"
	"unikraft.com/cli/internal/tui/meter"
)

// Usage represents a used/limit pair for quota tracking.
// It marshals to text as "used/limit".
type Usage[T any] struct {
	Used  T `field:",long" help:"Amount in use."`
	Limit T `field:",long" help:"Maximum allowed."`
}

func (u Usage[T]) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%v/%v", u.Used, u.Limit), nil
}

func (u Usage[T]) String() string {
	return fmt.Sprintf("%v/%v", u.Used, u.Limit)
}

func (u Usage[T]) Value() any {
	return u
}

// Range represents a min/max range for quota limits.
// It marshals to text as "min...max".
type Range[T any] struct {
	Min T `field:",long" help:"Smallest allowed value."`
	Max T `field:",long" help:"Largest allowed value."`
}

func (r Range[T]) MarshalText() ([]byte, error) {
	return fmt.Appendf(nil, "%v...%v", r.Min, r.Max), nil
}

func (r Range[T]) String() string {
	return fmt.Sprintf("%v...%v", r.Min, r.Max)
}

func (r Range[T]) Value() any {
	return r
}

type numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// MeterUsage represents a used/total pair for quotas, rendered as a colored
// meter bar.
type MeterUsage[T numeric] struct {
	Used  T `field:",long"`
	Total T `field:",long"`
}

// Compare compares two MeterUsage values by their used/total ratio, rounded
// to the same whole-percent resolution String renders (so sorting and
// filtering agree with what's displayed; e.g. "usage==69%" matches any
// value that displays as "69%", not just an exact 69/100 ratio), returning
// -1, 0, or 1 per the same contract as cmp.Compare.
func (u MeterUsage[T]) Compare(other MeterUsage[T]) int {
	return cmp.Compare(u.roundedRatio(), other.roundedRatio())
}

// ratio returns the used/total ratio, or 0 if Total is 0.
func (u MeterUsage[T]) ratio() float64 {
	if u.Total == 0 {
		return 0
	}
	return float64(u.Used) / float64(u.Total)
}

// roundedRatio returns the used/total ratio rounded to the nearest
// percentage point, matching the resolution String displays.
func (u MeterUsage[T]) roundedRatio() float64 {
	return math.Round(u.ratio()*100) / 100
}

// clampedRatio returns the used/total ratio clamped to [0, 1], or 0 if Total
// is 0.
func (u MeterUsage[T]) clampedRatio() float64 {
	ratio := u.ratio()
	if ratio > 1 {
		return 1
	}
	if ratio < 0 {
		return 0
	}
	return ratio
}

// String returns the used/total ratio as a raw percentage, e.g. "80%", or
// "" if Total is 0.
func (u MeterUsage[T]) String() string {
	if u.Total == 0 {
		return ""
	}
	return fmt.Sprintf("%.0f%%", u.clampedRatio()*100)
}

// Render implements value.Renderer, rendering the usage as a percentage
// followed by a colored meter bar, e.g. "80% ⣿⣿⣿⣿⣿⣿⣿⣿⣀⣀". In quiet mode
// it renders just the raw percentage, matching String.
func (u MeterUsage[T]) Render(opts value.RenderOpts) (string, error) {
	if opts.Quiet {
		return u.String(), nil
	}
	if u.Total == 0 {
		return "", nil
	}

	bar := meter.Render(u.clampedRatio(), meter.Width())

	// Right-pad the percentage to the width of "100%" so the bar stays on
	// the same column regardless of how many digits the percentage has.
	pct := fmt.Sprintf("%-4s", u.String())

	return fmt.Sprintf("%s %s", pct, bar), nil
}

func (u MeterUsage[T]) MarshalText() ([]byte, error) {
	if u.Total == 0 {
		return []byte(""), nil
	}
	return fmt.Appendf(nil, "%v/%v", u.Used, u.Total), nil
}

// UnmarshalText parses MeterUsage back from the "used/total" form
// MarshalText produces, round-tripping it. As a convenience for ordering
// filters (e.g. --filter "usage>0.5"), it also accepts a bare ratio (e.g.
// "0.5") or percentage (e.g. "50%"), building a used/total pair with a
// matching ratio at the same 1% resolution String renders at.
func (u *MeterUsage[T]) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	if s == "" {
		u.Used, u.Total = 0, 0
		return nil
	}

	if used, total, ok := strings.Cut(s, "/"); ok {
		usedVal, err := parseMeterUsagePart[T](used)
		if err != nil {
			return err
		}
		totalVal, err := parseMeterUsagePart[T](total)
		if err != nil {
			return err
		}
		u.Used, u.Total = usedVal, totalVal
		return nil
	}

	trimmed, isPercent := strings.CutSuffix(s, "%")
	ratio, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return err
	}
	if isPercent {
		ratio /= 100
	}
	const scale = 100
	u.Used = T(math.Round(ratio * scale))
	u.Total = T(scale)
	return nil
}

func parseMeterUsagePart[T numeric](s string) (T, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return T(v), nil
}
