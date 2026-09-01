// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package io

import (
	"io"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"unikraft.com/x/guesstermwidth"
)

// Unwrap peels off known io.Writer wrappers to expose the underlying writer.
func Unwrap(w io.Writer) io.Writer {
	for {
		switch ww := w.(type) {
		case *colorprofile.Writer:
			w = ww.Forward
		default:
			return w
		}
	}
}

func isTerminal(v any) bool {
	fd, ok := v.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return term.IsTerminal(fd.Fd())
}

// IsTTY reports whether the writer ultimately targets a terminal, transparently
// peeling off known wrappers via Unwrap.
func IsTTY(w io.Writer) bool {
	return isTerminal(Unwrap(w))
}

// IsTTYReader reports whether the reader ultimately draws from a terminal.
func IsTTYReader(r io.Reader) bool {
	return isTerminal(r)
}

// TermWidth returns the terminal width for the writer, transparently peeling
// off known wrappers via Unwrap.
func TermWidth(w io.Writer) int {
	return guesstermwidth.GuessTermWidth(Unwrap(w))
}
