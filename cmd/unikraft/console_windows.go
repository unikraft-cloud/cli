// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"os"

	"github.com/charmbracelet/colorprofile"
	"golang.org/x/sys/windows"
)

// colorWriter turns on VT processing for a console, so that it shows colours.
// A console that refuses VT mode gets plain text.
func colorWriter(f *os.File) *colorprofile.Writer {
	w := colorprofile.NewWriter(f, os.Environ())

	var mode uint32
	handle := windows.Handle(f.Fd())
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return w
	}
	if err := windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		w.Profile = colorprofile.NoTTY
	}
	return w
}
