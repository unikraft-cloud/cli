// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package watcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	xio "unikraft.com/cli/internal/x/io"
)

func WatchOutput(ctx context.Context, interval time.Duration, out io.Writer, render func(io.Writer) error) error {
	if xio.IsTTY(out) {
		return watchOutputPretty(ctx, interval, xio.Unwrap(out), render)
	}
	return watchOutputPlain(ctx, interval, out, render)
}

func watchOutputPlain(ctx context.Context, interval time.Duration, out io.Writer, render func(io.Writer) error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := render(out); err != nil {
			return err
		}
		fmt.Fprintln(out)

		select {
		case <-ctx.Done():
			err := ctx.Err()
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		case <-ticker.C:
		}
	}
}
