// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package time extends the standard library's duration and time parsing to
// be more lenient about the input formats they accept.
package time

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/araddon/dateparse"
)

// extendedUnitPattern matches the day/week/year units that time.ParseDuration
// doesn't natively support.
var extendedUnitPattern = regexp.MustCompile(`(\d+(?:\.\d+)?)(y|w|d)`)

// ParseDuration parses a duration string, additionally supporting "d" (day),
// "w" (week), and "y" (year) units on top of the units understood by
// time.ParseDuration.
func ParseDuration(s string) (time.Duration, error) {
	expanded := extendedUnitPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := extendedUnitPattern.FindStringSubmatch(match)
		n, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return match
		}
		var hours float64
		switch parts[2] {
		case "y":
			hours = n * 24 * 365
		case "w":
			hours = n * 24 * 7
		case "d":
			hours = n * 24
		}
		return strconv.FormatFloat(hours, 'f', -1, 64) + "h"
	})
	return time.ParseDuration(expanded)
}

// ParseTime parses a point in time without requiring the caller to know its
// format in advance (RFC3339, a bare date, a date and time with or without a
// zone offset, a Unix timestamp, and more). Inputs without a zone offset are
// interpreted as UTC, matching time.Parse's own default.
func ParseTime(s string) (time.Time, error) {
	return dateparse.ParseAny(strings.TrimSpace(s))
}
