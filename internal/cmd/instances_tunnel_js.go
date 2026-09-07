// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import (
	"context"

	"unikraft.com/cli/internal/config"
)

// Run reports that tunnels need a local listening socket.
func (cmd *InstancesTunnelCmd) Run(ctx context.Context, stdio config.Stdio) error {
	return notAvailable("instance tunnel")
}
