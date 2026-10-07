// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import (
	"context"
	"fmt"

	"github.com/alecthomas/kong"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
)

// notAvailable reports that a command needs the native CLI.
func notAvailable(what string) error {
	return fmt.Errorf(
		"%s is only available in the native unikraft CLI: https://unikraft.com/docs/cli",
		what,
	)
}

func (c *BuildCmd) Run(ctx context.Context, cfg *config.Config, partition *resource.Partition) error {
	return notAvailable("build")
}

func (c *ImageBuildCmd) Run(ctx context.Context, cfg *config.Config, partition *resource.Partition) error {
	return notAvailable("image build")
}

func (c *CompletionCmd) Run(ctx *kong.Context) error {
	return notAvailable("completion")
}

func (cmd *InstancesTunnelCmd) Run(ctx context.Context, stdio config.Stdio) error {
	return notAvailable("instance tunnel")
}

func (cmd *TUICmd) Run(ctx context.Context, stdio config.Stdio) error {
	return notAvailable("the TUI")
}

func (cmd *UpgradeCmd) Run(ctx context.Context, stdio config.Stdio) error {
	return notAvailable("upgrade")
}

func (c *VolumeImportCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	return notAvailable("volume import")
}
