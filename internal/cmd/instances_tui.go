// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
	"github.com/rs/zerolog"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/platform/logs"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/pkg/model"
	"unikraft.com/cli/pkg/model/cmd"
	modeltui "unikraft.com/cli/pkg/model/tui"
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

	// Logs on the terminal break the view, so the lineage is read without them.
	historyFunc := func(ctx context.Context) (io.ReadCloser, error) {
		nop := zerolog.Nop()
		ctx = log.WithLogger(ctx, &nop)
		found, err := Instance{}.Get(ctx, []string{key})
		if len(found) == 0 {
			return nil, cmp.Or(err, fmt.Errorf("instance %s not found", key))
		}
		inst := found[0].(Instance)
		target := lineageTarget{key: inst.key, inst: inst.Instance}
		lins, err := readInstanceLineages(ctx, []lineageTarget{target}, 0)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := printLineageTables(ctx, &buf, cmd.Printer{Type: cmd.PrinterTypeTable}, nil, lins); err != nil {
			return nil, err
		}
		return io.NopCloser(&buf), nil
	}

	return []tea.Model{
		modeltui.NewReaderPanelFunc(ctx, "Logs", readerFunc, true),
		modeltui.NewReaderPanelFunc(ctx, "History", historyFunc, false).LoadOnFocus(),
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
		if _, ok := errors.AsType[model.ErrNotFound](err); ok {
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
