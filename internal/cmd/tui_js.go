// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import (
	"context"

	"unikraft.com/cli/internal/config"
)

type TUICmd struct {
	Resource string `arg:"" optional:"" help:"Resource type to browse."`
	Name     string `arg:"" optional:"" help:"Resource key to open."`
}

// Run reports that the full-screen TUI needs a real terminal.
func (cmd *TUICmd) Run(ctx context.Context, stdio config.Stdio) error {
	return notAvailable("the TUI")
}
