// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package rollout

import (
	"context"
	"fmt"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/types"
)

// Platform defaults for an instance that sets neither, as the create request
// documents them in the SDK.
const (
	defaultVcpus    = 1
	defaultMemoryMb = 128
)

// ValidateCapacity reports why the metro cannot take count more instances
// while the ones they replace still run. The platform enforces the real limits.
func ValidateCapacity(ctx context.Context, metro string, autoscale bool, count int64, fields []resource.Field) error {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	var quotas platform.Quotas
	if err := group.DoMetro(ctx, g, metro, func(ctx context.Context, mc multimetro.MetroClient) error {
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
		log.G(ctx).Debug().Err(err).Msg("skipping the rollout capacity check")
		return nil
	}

	vcpus := int64(defaultVcpus)
	if v, ok := FieldValue[int](fields, "resources.vcpus"); ok && v > 0 {
		vcpus = int64(v)
	}
	memory := int64(defaultMemoryMb)
	if v, ok := FieldValue[types.SizeMebibytes](fields, "resources.memory"); ok && v > 0 {
		memory = int64(v)
	}

	for _, q := range []struct {
		what      string
		used, max int64
		need      int64
	}{
		{"instances", quotas.Used.Instances, quotas.Hard.Instances, count},
		{"live instances", quotas.Used.LiveInstances, quotas.Hard.LiveInstances, count},
		{"live vCPUs", quotas.Used.LiveVcpus, quotas.Hard.LiveVcpus, count * vcpus},
		{"live memory in MiB", quotas.Used.LiveMemoryMb, quotas.Hard.LiveMemoryMb, count * memory},
	} {
		if q.max > 0 && q.used+q.need > q.max {
			return fmt.Errorf("not enough room to roll %d instance(s): %d of %d %s in use, the rollout needs %d more", count, q.used, q.max, q.what, q.need)
		}
	}
	if limit := quotas.Limits.MaxAutoscaleSize; autoscale && limit > 0 && 2*count > limit {
		return fmt.Errorf("not enough room to roll %d instance(s): both sets add up to %d instances, over the maximum autoscale group size of %d", count, 2*count, limit)
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
		Msgf("rollout failed, deleting the new %ss", name)
	if err := partition.WrapDeletable(r).Delete(ctx, keys); err != nil {
		return fmt.Errorf("deleting the new %ss after a failed rollout: %w", name, err)
	}
	return nil
}

// FieldValue reads the value a create sets at path.
func FieldValue[T any](fields []resource.Field, path string) (T, bool) {
	for _, field := range resource.GetFieldByPathString(fields, path) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		if v, ok := field.Create.Set.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}
