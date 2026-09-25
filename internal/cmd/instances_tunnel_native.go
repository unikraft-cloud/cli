// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"fmt"
	"time"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/tunnel"
)

func (cmd *InstancesTunnelCmd) Run(ctx context.Context, stdio config.Stdio) error {
	targets, err := tunnel.ParseTargets(cmd.Targets, cmd.TunnelProxyPorts)
	if err != nil {
		return fmt.Errorf("could not parse targets: %w", err)
	}

	tun, err := tunnel.New(ctx, targets)
	if err != nil {
		return fmt.Errorf("could not create tunnel: %w", err)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	defer func() {
		// Detach from ctx so cleanup survives the same Ctrl-C that triggered
		// it, but bound it so a stuck API call can't leave a still-billed
		// proxy instance orphaned.
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := tun.Close(closeCtx, g); err != nil {
			log.G(ctx).Error().Err(err).Msg("could not terminate tunnel proxy")
		}
	}()

	return tun.Run(ctx, g, cmd.ProxyControlPort, cmd.TunnelServiceImage)
}
