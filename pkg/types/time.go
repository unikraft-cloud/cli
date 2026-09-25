// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// RelativeTime is a time wrapper that represents time in a human-friendly
// relative format (e.g., "2 hours ago", "in 5 minutes").
type RelativeTime time.Time

// Now returns the current time as a RelativeTime.
func Now() RelativeTime {
	return RelativeTime(time.Now())
}

func (t *RelativeTime) UnmarshalText(text []byte) error {
	s := string(text)

	// Try parsing as RFC3339 first
	if parsed, err := time.Parse(time.RFC3339, s); err == nil {
		*t = RelativeTime(parsed)
		return nil
	}

	// Try parsing as RFC3339Nano
	if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
		*t = RelativeTime(parsed)
		return nil
	}

	// Try parsing as Unix timestamp (seconds)
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		*t = RelativeTime(time.Unix(ts, 0))
		return nil
	}

	return fmt.Errorf("invalid time %q: expected RFC3339, RFC3339Nano, or Unix timestamp", s)
}

func (t *RelativeTime) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return t.UnmarshalText([]byte(text))
	}
	return t.UnmarshalText(data)
}

func (t RelativeTime) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

func (t RelativeTime) MarshalJSON() ([]byte, error) {
	text, err := t.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

// String returns the time as a human-friendly relative string.
func (t RelativeTime) String() string {
	return formatRelativeTime(time.Time(t), time.Now())
}

// Time returns the underlying time.Time value.
func (t RelativeTime) Time() time.Time {
	return time.Time(t)
}

// IsZero reports whether t represents the zero time instant.
func (t RelativeTime) IsZero() bool {
	return time.Time(t).IsZero()
}

// formatRelativeTime formats the given time relative to now.
func formatRelativeTime(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}

	diff := now.Sub(t)
	future := diff < 0
	if future {
		diff = -diff
	}

	var result string
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			result = "1 minute"
		} else {
			result = strconv.Itoa(mins) + " minutes"
		}
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			result = "1 hour"
		} else {
			result = strconv.Itoa(hours) + " hours"
		}
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			result = "1 day"
		} else {
			result = strconv.Itoa(days) + " days"
		}
	case diff < 30*24*time.Hour:
		weeks := int(diff.Hours() / 24 / 7)
		if weeks == 1 {
			result = "1 week"
		} else {
			result = strconv.Itoa(weeks) + " weeks"
		}
	case diff < 365*24*time.Hour:
		months := int(diff.Hours() / 24 / 30)
		if months == 1 {
			result = "1 month"
		} else {
			result = strconv.Itoa(months) + " months"
		}
	default:
		years := int(diff.Hours() / 24 / 365)
		if years == 1 {
			result = "1 year"
		} else {
			result = strconv.Itoa(years) + " years"
		}
	}

	if future {
		return "in " + result
	}
	return result + " ago"
}
