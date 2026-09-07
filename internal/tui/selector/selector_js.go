// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package selector

import jujuerrors "github.com/juju/errors"

var ErrNoOptionSelected = jujuerrors.New("no option selected")

// ErrNotInteractive reports that the caller must name its choice up front.
var ErrNotInteractive = jujuerrors.New("interactive selection is not available in the web console; pass the value as an argument")

func Single[T ~string](question string, options ...T) (T, error) {
	var zero T
	return zero, ErrNotInteractive
}

func SingleWithDefault[T ~string](question string, defaultValue string, options ...T) (T, error) {
	var zero T
	return zero, ErrNotInteractive
}
