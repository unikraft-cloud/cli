// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package model

import "strings"

// ErrNotFound is returned when some of the requested keys don't exist.
type ErrNotFound struct {
	Keys []Key
	Err  error
}

func (e ErrNotFound) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	keys := make([]string, len(e.Keys))
	for i, k := range e.Keys {
		keys[i] = k.String()
	}
	return "not found: " + strings.Join(keys, ", ")
}

func (e ErrNotFound) Unwrap() error {
	return e.Err
}
