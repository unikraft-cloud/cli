// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package watcher

import (
	"context"
	"io"
	"time"
)

// watchOutputPretty falls back to plain re-rendering.  The browser terminal
// owns the screen, so the alternate-screen view is not used there.
func watchOutputPretty(ctx context.Context, interval time.Duration, out io.Writer, render func(io.Writer) error) error {
	return watchOutputPlain(ctx, interval, out, render)
}
