// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"encoding/json"
	"strconv"
	"time"
)

// DurationS is a time wrapper that represents a duration in seconds.
type DurationS time.Duration

func (d *DurationS) UnmarshalText(text []byte) error {
	if dur, err := strconv.Atoi(string(text)); err == nil {
		*d = DurationS(time.Duration(dur))
		return nil
	}
	dur, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	// Round to the next full second
	secs := dur / time.Second
	switch rem := dur % time.Second; {
	case rem > 0:
		secs++
	case rem < 0:
		secs--
	}
	*d = DurationS(secs)
	return nil
}

func (d *DurationS) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return d.UnmarshalText([]byte(text))
	}
	return d.UnmarshalText(data)
}

func (d DurationS) MarshalText() ([]byte, error) {
	return []byte((time.Duration(d) * time.Second).String()), nil
}

func (d DurationS) MarshalJSON() ([]byte, error) {
	text, err := d.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

func (d DurationS) String() string {
	return (time.Duration(d) * time.Second).String()
}

// DurationMS is a time wrapper that represents a duration in milliseconds.
type DurationMS int64

func (d *DurationMS) UnmarshalText(text []byte) error {
	if ms, err := strconv.Atoi(string(text)); err == nil {
		*d = DurationMS(ms)
		return nil
	}
	dur, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = DurationMS(dur.Milliseconds())
	return nil
}

func (d *DurationMS) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return d.UnmarshalText([]byte(text))
	}
	return d.UnmarshalText(data)
}

func (d DurationMS) MarshalText() ([]byte, error) {
	return []byte((time.Duration(d) * time.Millisecond).String()), nil
}

func (d DurationMS) MarshalJSON() ([]byte, error) {
	text, err := d.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

func (d DurationMS) String() string {
	dur := time.Duration(d) * time.Millisecond
	if d >= 1000 {
		dur = dur.Round(10 * time.Millisecond)
	}
	return dur.String()
}

// DurationUS is a time wrapper that represents a duration in microseconds.
type DurationUS int64

func (d *DurationUS) UnmarshalText(text []byte) error {
	if us, err := strconv.Atoi(string(text)); err == nil {
		*d = DurationUS(us)
		return nil
	}
	dur, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = DurationUS(dur.Microseconds())
	return nil
}

func (d *DurationUS) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return d.UnmarshalText([]byte(text))
	}
	return d.UnmarshalText(data)
}

func (d DurationUS) MarshalText() ([]byte, error) {
	return []byte((time.Duration(d) * time.Microsecond).String()), nil
}

func (d DurationUS) MarshalJSON() ([]byte, error) {
	text, err := d.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

func (d DurationUS) String() string {
	dur := time.Duration(d) * time.Microsecond
	switch {
	case d >= 1_000_000: // >= 1s: round to 10 milliseconds
		dur = dur.Round(10 * time.Millisecond)
	case d >= 1000: // >= 1ms: round to 10 microseconds
		dur = dur.Round(10 * time.Microsecond)
	}
	return dur.String()
}
