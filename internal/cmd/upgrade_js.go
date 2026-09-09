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

type UpgradeCmd struct {
	Channel string `help:"Release channel to upgrade from." default:"stable" enum:"stable,staging"`
	Force   bool   `short:"f" help:"Force upgrade even if already at latest version."`
	Version string `short:"v" help:"Upgrade to a specific version."`
	BinDir  string `help:"Directory where to install the binary. If empty, uses the current binary location."`
	BaseUrl string `help:"Base URL for fetching releases." env:"UNIKRAFT_CLI_INSTALL_URL" default:"https://pkg.unikraft.com" hidden:"true"`
}

// Run reports that self-upgrade needs a writable binary on disk.
func (cmd *UpgradeCmd) Run(ctx context.Context, stdio config.Stdio) error {
	return notAvailable("upgrade")
}
