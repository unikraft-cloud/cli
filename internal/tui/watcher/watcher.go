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

	tea "charm.land/bubbletea/v2"

	xio "unikraft.com/x/io"
)

func WatchOutput(ctx context.Context, interval time.Duration, out io.Writer, render func(io.Writer) error) error {
	if xio.IsTTY(out) {
		return watchOutputPretty(ctx, interval, xio.Unwrap(out), render)
	}
	return watchOutputPlain(ctx, interval, out, render)
}

func watchOutputPretty(ctx context.Context, interval time.Duration, out io.Writer, render func(io.Writer) error) error {
	program := tea.NewProgram(
		watchModel{underlying: out, render: render, interval: interval},
		tea.WithOutput(out),
		tea.WithContext(ctx),
	)

	finalModel, err := program.Run()
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	if model, ok := finalModel.(watchModel); ok && model.err != nil {
		return model.err
	}
	return nil
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
