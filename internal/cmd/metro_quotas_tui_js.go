// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import (
	"context"
	"time"

	"unikraft.com/cli/internal/config"
)

// runQuotasTUI falls back to plain rendering.  The browser terminal owns the
// screen, so the full-screen view is not used there.
func runQuotasTUI(ctx context.Context, stdio config.Stdio, cmd *MetroQuotasCmd, watch *time.Duration) error {
	if watch != nil {
		return cmd.watchRenderLoop(ctx, stdio.Stdout, *watch)
	}
	return cmd.renderOnce(ctx, stdio.Stdout)
}
