// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"fmt"

	"github.com/docker/go-units"
	"github.com/moby/buildkit/client"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/buildkit"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
)

var errNoBuilder = fmt.Errorf("there is no builder %s: create it with unikraft buildkit create", builderName)

func (c *BuildkitDuCmd) Run(ctx context.Context, stdio config.Stdio) error {
	// NewClient reports a bad profile, which defaultMetro hides.
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	metro := defaultMetro(ctx, c.Metro)
	if metro == "" {
		return errNoMetro
	}
	return group.DoMetro(ctx, g, metro, func(ctx context.Context, mc multimetro.MetroClient) error {
		p, exists, err := builder(ctx, mc)
		if err != nil {
			return err
		}
		if !exists {
			return errNoBuilder
		}
		d, err := p.Acquire(ctx, buildkit.Key{})
		if err != nil {
			return err
		}
		defer func() { _ = d.Release() }()

		du, err := d.Client.DiskUsage(ctx)
		if err != nil {
			return err
		}
		var reclaimable, total int64
		for _, u := range du {
			if u.Size <= 0 {
				continue
			}
			total += u.Size
			if !u.InUse {
				reclaimable += u.Size
			}
		}
		_, err = fmt.Fprintf(stdio.Stdout, "Reclaimable: %s\nTotal:       %s\n", units.BytesSize(float64(reclaimable)), units.BytesSize(float64(total)))
		return err
	})
}

func (c *BuildkitPruneCmd) Run(ctx context.Context) error {
	// NewClient reports a bad profile, which defaultMetro hides.
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	metro := defaultMetro(ctx, c.Metro)
	if metro == "" {
		return errNoMetro
	}
	return group.DoMetro(ctx, g, metro, func(ctx context.Context, mc multimetro.MetroClient) error {
		p, exists, err := builder(ctx, mc)
		if err != nil {
			return err
		}
		if !exists {
			return errNoBuilder
		}
		d, err := p.Acquire(ctx, buildkit.Key{})
		if err != nil {
			return err
		}
		defer func() { _ = d.Release() }()

		// Prune sends each record it frees and never closes ch.
		ch := make(chan client.UsageInfo)
		freed := make(chan int64)
		go func() {
			var n int64
			for u := range ch {
				n += max(u.Size, 0)
			}
			freed <- n
		}()
		err = d.Client.Prune(ctx, ch)
		close(ch)
		n := <-freed
		if err != nil {
			return err
		}
		log.G(ctx).Info().Str("freed", units.BytesSize(float64(n))).Msg("pruned the builder's cache")
		return nil
	})
}
