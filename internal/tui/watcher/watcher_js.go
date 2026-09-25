// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package watcher

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func watchOutputPretty(ctx context.Context, interval time.Duration, out io.Writer, render func(io.Writer) error) error {
	return watchOutputPlain(ctx, interval, out, func(w io.Writer) error {
		var frame bytes.Buffer
		if err := render(&frame); err != nil {
			return err
		}
		_, err := io.WriteString(w, ansi.CursorHomePosition+ansi.EraseEntireScreen+frame.String())
		return err
	})
}
