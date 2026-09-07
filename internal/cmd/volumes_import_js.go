// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import (
	"context"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
)

// Run reports that volume imports need local files and a raw TCP connection.
func (c *VolumeImportCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	return notAvailable("volume import")
}
