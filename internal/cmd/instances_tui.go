// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/platform/logs"

	"unikraft.com/cli/internal/multimetro"
	resourcetui "unikraft.com/cli/internal/resource/tui"
)

// Subpanels returns subpanels for instance detail views.
func (Instance) Subpanels(ctx context.Context, key string) []tea.Model {
	parsed := multimetro.ParseKey(key)
	ref := parsed.Ref()
	id := ref.NameOrUUID()

	readerFunc := func(ctx context.Context) (io.ReadCloser, error) {
		client, err := instanceClient(ctx, ref)
		if err != nil {
			return nil, err
		}
		r, err := logs.InstanceLogs(ctx, client).Reader(id, new(100), true)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(r), nil
	}

	return []tea.Model{
		resourcetui.NewReaderPanelFunc(ctx, "Logs", readerFunc, true),
	}
}

func instanceClient(ctx context.Context, ref group.Ref) (platform.Client, error) {
	clients, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	if ref.Metro != "" {
		client, err := group.CollectMetro(ctx, clients, ref.Metro, func(ctx context.Context, c multimetro.MetroClient) (platform.Client, error) {
			return c.Client, nil
		})
		if err != nil {
			return nil, err
		}
		return client, nil
	}

	results, err := group.CollectRefs(ctx, clients, group.Refs{ref}, func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (platform.Client, group.Refs, error) {
		if len(refs) == 0 {
			return nil, nil, nil
		}
		resp, err := c.GetInstances(ctx, refs.NameOrUUIDs(), platform.GetInstancesOpts{Details: new(false)})
		if err != nil {
			// Treat errors as "not found" so other metros can still succeed.
			return nil, nil, nil
		}
		if resp == nil || resp.Data == nil || len(resp.Data.Instances) == 0 {
			return nil, nil, nil
		}
		for _, inst := range resp.Data.Instances {
			if inst.Status == nil || *inst.Status != platform.ResponseStatusSuccess {
				continue
			}
			return c.Client, refs, nil
		}
		return nil, nil, nil
	})
	if err != nil {
		if _, ok := errors.AsType[group.ErrRefNotFound](err); ok {
			return nil, fmt.Errorf("instance not found")
		}
		return nil, err
	}

	for _, client := range results {
		if client != nil {
			return client, nil
		}
	}
	return nil, fmt.Errorf("instance not found")
}
