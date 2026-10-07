// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package time

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"2w", 2 * 7 * 24 * time.Hour},
		{"1y", 365 * 24 * time.Hour},
		{"2w1d", 2*7*24*time.Hour + 24*time.Hour},
		{"3d1h", 3*24*time.Hour + time.Hour},
		{"1.5d", 36 * time.Hour},
		{"30m", 30 * time.Minute},
		{"1h30m", time.Hour + 30*time.Minute},
		{"-7d", -7 * 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := ParseDuration(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, d)
		})
	}

	t.Run("invalid", func(t *testing.T) {
		_, err := ParseDuration("notaduration")
		assert.Error(t, err)
	})
}

func TestParseTime(t *testing.T) {
	expected := time.Date(2024, 3, 5, 14, 30, 15, 0, time.UTC)

	tests := []struct {
		name  string
		input string
	}{
		{"rfc3339", "2024-03-05T14:30:15Z"},
		{"rfc3339_offset", "2024-03-05T16:30:15+02:00"},
		{"rfc3339_nano", "2024-03-05T14:30:15.000000000Z"},
		{"no_zone", "2024-03-05T14:30:15"},
		{"date_time_space", "2024-03-05 14:30:15"},
		{"whitespace", "  2024-03-05T14:30:15Z  "},
		{"slash_date", "2024/03/05 14:30:15"},
		{"go_string", "2024-03-05 14:30:15 +0000 UTC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTime(tt.input)
			require.NoError(t, err)
			assert.True(t, expected.Equal(got), "expected %v, got %v", expected, got)
		})
	}

	t.Run("date_only", func(t *testing.T) {
		got, err := ParseTime("2024-03-05")
		require.NoError(t, err)
		assert.True(t, time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC).Equal(got))
	})

	t.Run("unix_seconds", func(t *testing.T) {
		got, err := ParseTime(strconv.FormatInt(expected.Unix(), 10))
		require.NoError(t, err)
		assert.True(t, expected.Equal(got))
	})

	t.Run("invalid", func(t *testing.T) {
		_, err := ParseTime("notatime")
		assert.Error(t, err)
	})
}
