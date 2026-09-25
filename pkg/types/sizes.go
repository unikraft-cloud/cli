// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	"encoding/json"
	"strconv"

	"github.com/docker/go-units"
)

// SizeMebibytes is a size wrapper that represents a size in mebibytes.
type SizeMebibytes int64

func (m *SizeMebibytes) UnmarshalText(text []byte) error {
	if d, err := strconv.Atoi(string(text)); err == nil {
		*m = SizeMebibytes(d)
		return nil
	}
	size, err := units.RAMInBytes(string(text))
	if err != nil {
		return err
	}
	*m = SizeMebibytes(size / units.MiB)
	return nil
}

func (m *SizeMebibytes) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return m.UnmarshalText([]byte(text))
	}
	return m.UnmarshalText(data)
}

func (m SizeMebibytes) MarshalText() ([]byte, error) {
	return []byte(units.BytesSize(float64(m) * units.MiB)), nil
}

func (m SizeMebibytes) MarshalJSON() ([]byte, error) {
	text, err := m.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

func (m SizeMebibytes) String() string {
	return units.BytesSize(float64(m) * units.MiB)
}

// SizeBytes is a size wrapper that represents a size in bytes.
type SizeBytes int64

func (b *SizeBytes) UnmarshalText(text []byte) error {
	if d, err := strconv.ParseInt(string(text), 10, 64); err == nil {
		*b = SizeBytes(d)
		return nil
	}
	value, err := units.FromHumanSize(string(text))
	if err != nil {
		return err
	}
	*b = SizeBytes(value)
	return nil
}

func (b *SizeBytes) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return b.UnmarshalText([]byte(text))
	}
	return b.UnmarshalText(data)
}

func (b SizeBytes) MarshalText() ([]byte, error) {
	return []byte(units.BytesSize(float64(b))), nil
}

func (b SizeBytes) MarshalJSON() ([]byte, error) {
	text, err := b.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

func (b SizeBytes) String() string {
	return units.BytesSize(float64(b))
}
