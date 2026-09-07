// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package watcher

import (
	"context"
	"errors"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
)

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
