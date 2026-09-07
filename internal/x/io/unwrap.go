// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package io

import (
	"io"

	"github.com/charmbracelet/colorprofile"
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
