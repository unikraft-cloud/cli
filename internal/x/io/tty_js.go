// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package io

import (
	"io"
	"os"
	"strconv"
)

// forceTTYEnv makes the host declare that stdout is a terminal.  The browser
// terminal renders ANSI output but has no file descriptor to probe.
const forceTTYEnv = "UNIKRAFT_FORCE_TTY"

// defaultTermWidth is used when the host does not report a width.
const defaultTermWidth = 80

// IsTTY reports whether the host declared its output to be a terminal.
func IsTTY(w io.Writer) bool {
	return os.Getenv(forceTTYEnv) == "1"
}

// IsTTYReader always reports false.  Standard input is a pipe fed by the host,
// so callers must read it rather than skip it.
func IsTTYReader(r io.Reader) bool {
	return false
}

// TermWidth returns the width the host reported in COLUMNS.
func TermWidth(w io.Writer) int {
	if cols, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && cols > 0 {
		return cols
	}
	return defaultTermWidth
}
