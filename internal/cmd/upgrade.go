// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"os"
	"path/filepath"
	"runtime"
)

type UpgradeCmd struct {
	Channel string `help:"Release channel to upgrade from." default:"stable" enum:"stable,staging"`
	Force   bool   `short:"f" help:"Force upgrade even if already at latest version."`
	Version string `short:"v" help:"Upgrade to a specific version."`
	BinDir  string `help:"Directory where to install the binary. If empty, uses the current binary location."`
	BaseUrl string `help:"Base URL for fetching releases." env:"UNIKRAFT_CLI_INSTALL_URL" default:"https://pkg.unikraft.com" hidden:"true"`
}

// RemoveUpgradeBackup removes the old binary that an upgrade on Windows keeps.
// Windows cannot remove a running binary, thus the next run removes it.
func RemoveUpgradeBackup() {
	if runtime.GOOS != "windows" {
		return
	}
	execPath, err := os.Executable()
	if err != nil {
		return
	}
	if execPath, err = filepath.EvalSymlinks(execPath); err != nil {
		return
	}
	_ = os.Remove(upgradeBackupPath(execPath))
}

// upgradeBackupPath returns the path where an upgrade keeps the old binary.
func upgradeBackupPath(binPath string) string {
	return filepath.Join(filepath.Dir(binPath), "."+filepath.Base(binPath)+".old")
}
