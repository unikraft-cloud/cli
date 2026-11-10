// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package graph

import (
	"math"
	"strconv"
	"strings"
)

// Unit selects how values are scaled and labelled.
type Unit int

const (
	// UnitCount is a plain number, such as open connections.
	UnitCount Unit = iota
	// UnitBytes is a size in bytes, labelled with IEC prefixes.
	UnitBytes
	// UnitBytesPerSecond is a throughput in bytes per second.
	UnitBytesPerSecond
	// UnitPerSecond is a plain rate, such as requests per second.
	UnitPerSecond
	// UnitPercent is a percentage.
	UnitPercent
)

// minTop is the smallest value an axis can end at, so that a flat zero line
// still has some height.
const minTop = 1.0

// niceLadder lists the mantissas an axis can end at, per power of ten.
var niceLadder = []float64{1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10}

// niceCeil returns the smallest ladder value that is at least x.
func niceCeil(x float64) float64 {
	if x <= 0 || math.IsNaN(x) || math.IsInf(x, 0) {
		return 1
	}
	base := math.Pow(10, math.Floor(math.Log10(x)))
	mantissa := x / base
	for _, step := range niceLadder {
		if step >= mantissa-1e-9 {
			return step * base
		}
	}
	return 10 * base
}

// binary reports whether the unit is scaled by powers of 1024.
func (u Unit) binary() bool {
	return u == UnitBytes || u == UnitBytesPerSecond
}

// axisTop returns the value at the top of the axis for a series whose largest
// value is maxV. A positive ceiling is used as it is, unless the data is above
// it.
func axisTop(maxV float64, unit Unit, ceiling float64) float64 {
	if ceiling > 0 && maxV <= ceiling {
		return ceiling
	}
	scale := 1.0
	if unit.binary() {
		for maxV/scale >= 1024 && scale < 1<<40 {
			scale *= 1024
		}
	}
	nice := niceCeil(maxV / scale)
	if unit.binary() && nice >= 1000 {
		nice = 1024
	}
	return max(nice*scale, minTop)
}

// sig formats v with at most n significant figures and no trailing zeros.
func sig(v float64, n int) string {
	if v == 0 {
		return "0"
	}
	digits := int(math.Floor(math.Log10(math.Abs(v)))) + 1
	s := strconv.FormatFloat(v, 'f', max(0, n-digits), 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

var (
	binaryPrefixes = []string{"", "Ki", "Mi", "Gi", "Ti"}
	decimalSuffix  = []string{"", "k", "M", "G"}
)

// scaleBinary divides v by 1024 until it is below 1024.
func scaleBinary(v float64) (float64, int) {
	i := 0
	for math.Abs(v) >= 1024 && i < len(binaryPrefixes)-1 {
		v /= 1024
		i++
	}
	return v, i
}

// scaleDecimal divides v by 1000 until it is below 1000.
func scaleDecimal(v float64) (float64, int) {
	i := 0
	for math.Abs(v) >= 1000 && i < len(decimalSuffix)-1 {
		v /= 1000
		i++
	}
	return v, i
}

// FormatReading formats v for text next to a chart, such as "12.5 MiB".
// Percentages, counts and plain rates carry no unit text, because the title
// of a chart names it.
func FormatReading(v float64, unit Unit) string {
	switch unit {
	case UnitBytes, UnitBytesPerSecond:
		scaled, i := scaleBinary(v)
		out := sig(scaled, 3) + " " + binaryPrefixes[i] + "B"
		if unit == UnitBytesPerSecond {
			out += "/s"
		}
		return out
	case UnitPercent, UnitPerSecond, UnitCount:
		scaled, i := scaleDecimal(v)
		return sig(scaled, 3) + decimalSuffix[i]
	}
	return sig(v, 3)
}

// axisLabelWidth is the width of the value gutter.
const axisLabelWidth = 5

// axisLabels formats the labels of one axis in one form. If a label does not
// fit with a full prefix, such as "1.25Mi", every label uses the short form,
// such as "1.25M", so that one axis never mixes "Mi" and "M".
func axisLabels(values []float64, unit Unit) []string {
	short := false
	for _, v := range values {
		if _, ok := axisLabelForm(v, unit, false); !ok {
			short = true
		}
	}
	labels := make([]string, len(values))
	for i, v := range values {
		labels[i], _ = axisLabelForm(v, unit, short)
	}
	return labels
}

// axisLabelForm formats v with the full prefix, or without the "i" of a
// binary prefix when short is set. It reports false when the label is too
// wide, and then returns a label of two significant figures.
func axisLabelForm(v float64, unit Unit, short bool) (string, bool) {
	var number, prefix string
	if unit.binary() {
		scaled, i := scaleBinary(v)
		number, prefix = sig(scaled, 3), binaryPrefixes[i]
		if short {
			prefix = strings.TrimSuffix(prefix, "i")
		}
	} else {
		scaled, i := scaleDecimal(v)
		number, prefix = sig(scaled, 3), decimalSuffix[i]
	}
	if label := number + prefix; len(label) <= axisLabelWidth {
		return label, true
	}
	return sig(v, 2), false
}
