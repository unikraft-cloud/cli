// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !windows

package main

import (
	"os"

	"github.com/charmbracelet/colorprofile"
)

// colorWriter wraps a stream so that it shows the colours its terminal supports.
func colorWriter(f *os.File) *colorprofile.Writer {
	return colorprofile.NewWriter(f, os.Environ())
}
