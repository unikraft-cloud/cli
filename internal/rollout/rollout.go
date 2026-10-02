// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package rollout

import (
	"context"
	"fmt"
	"slices"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
)

// Footprint is the live capacity a set of instances holds.
type Footprint struct {
	Instances int64
	Vcpus     int64
	MemoryMb  int64
}

// Known reports whether the footprint carries a per-instance size.
func (f Footprint) Known() bool {
	return f.Vcpus > 0 && f.MemoryMb > 0
}

// CapacityOpts describes the capacity one rollout needs.
type CapacityOpts struct {
	Metro string
	// Count is how many instances the rollout creates.
	Count int64
	// Concurrent says the new instances run alongside the ones they replace,
	// so the rollout gets none of the replaced capacity back.
	Concurrent bool
	// Size is what one new instance takes.
	Size Footprint
	// Freed is the live capacity the replaced instances give up.
	Freed Footprint
}

// ValidateCapacity reports why the metro cannot take the instances a rollout
// adds. The platform enforces the real limits.
func ValidateCapacity(ctx context.Context, opts CapacityOpts) error {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(g.Names(), opts.Metro) {
		return fmt.Errorf("metro %q is not configured", opts.Metro)
	}

	var quotas platform.Quotas
	if err := group.DoMetro(ctx, g, opts.Metro, func(ctx context.Context, mc multimetro.MetroClient) error {
		log.G(ctx).Trace().Msg("fetching quotas for the rollout")
		resp, err := mc.GetUser(ctx)
		if err != nil {
			return err
		}
		if resp.Data == nil || len(resp.Data.Quotas) == 0 {
			return fmt.Errorf("no quota data")
		}
		quotas = resp.Data.Quotas[0]
		return nil
	}); err != nil {
		log.G(ctx).Warn().Err(err).
			Msg("cannot read the quotas, skipping rollout capacity check")
		return nil
	}
	if !opts.Size.Known() {
		log.G(ctx).Warn().
			Msg("checking only the instance count")
	}
	return checkQuotas(quotas, opts)
}

// quotaRow is one quota a rollout is checked against.
type quotaRow struct {
	what      string
	used, max int64
	need      int64
}

// checkQuotas reports which quota the instances a rollout adds go over. A
// rollout that stops the instances it replaces gets their capacity back.
func checkQuotas(quotas platform.Quotas, opts CapacityOpts) error {
	freed := opts.Freed
	if opts.Concurrent {
		freed = Footprint{}
	}
	rows := []quotaRow{
		{"instances", quotas.Used.Instances, quotas.Hard.Instances, opts.Count},
		{"live instances", quotas.Used.LiveInstances, quotas.Hard.LiveInstances, opts.Count - freed.Instances},
	}
	if opts.Size.Known() {
		rows = append(rows,
			quotaRow{"live vCPUs", quotas.Used.LiveVcpus, quotas.Hard.LiveVcpus, opts.Count*opts.Size.Vcpus - freed.Vcpus},
			quotaRow{"live memory in MiB", quotas.Used.LiveMemoryMb, quotas.Hard.LiveMemoryMb, opts.Count*opts.Size.MemoryMb - freed.MemoryMb},
		)
	}
	for _, q := range rows {
		if q.need > 0 && q.max > 0 && q.used+q.need > q.max {
			return fmt.Errorf("not enough room to roll %d instance(s): %d of %d %s in use, the rollout needs %d more", opts.Count, q.used, q.max, q.what, q.need)
		}
	}
	return nil
}

// Undo deletes the resources a failed rollout created, so the rollout leaves
// only the resources it started with.
func Undo(ctx context.Context, partition *resource.Partition, r resource.DeletableResource, created []resource.Resource) error {
	if len(created) == 0 {
		return nil
	}
	keys := make([]string, len(created))
	for i, res := range created {
		keys[i] = res.Key().String()
	}
	name := r.Type().Name
	log.G(ctx).Warn().
		Strs(name+"s", keys).
		Msgf("unwinding the rollout, deleting the new %ss", name)
	if err := partition.WrapDeletable(r).Delete(ctx, keys); err != nil {
		return fmt.Errorf("deleting the new %ss after an unfinished rollout: %w", name, err)
	}
	return nil
}
