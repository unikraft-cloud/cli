// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import (
	"context"
	"io"
	"time"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/tui/watcher"
)

func runQuotasTUI(ctx context.Context, stdio config.Stdio, cmd *MetroQuotasCmd, watch *time.Duration) error {
	if watch == nil {
		return cmd.renderOnce(ctx, stdio.Stdout)
	}
	return watcher.WatchOutput(ctx, *watch, stdio.Stdout, func(w io.Writer) error {
		return cmd.renderOnce(ctx, w)
	})
}
