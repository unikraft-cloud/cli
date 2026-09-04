// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"maps"

	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
)

// defaultMetro returns the metro if non-empty, otherwise falls back to the
// default metro from the current profile in the context.
func defaultMetro(ctx context.Context, metro string) string {
	if metro != "" {
		return metro
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return ""
	}
	return profile.GetDefaultMetro()
}

func getFromListable(ctx context.Context, listable resource.ListableResource, keys []string) ([]resource.Resource, error) {
	all, err := listable.List(ctx)
	if err != nil {
		return nil, err
	}

	found := make([]resource.Resource, 0, len(keys))
	var notFound []string
loop:
	for _, key := range keys {
		for _, resource := range all {
			if resource.Key().String() == key {
				found = append(found, resource)
				continue loop
			}
		}
		notFound = append(notFound, key)
	}

	if len(notFound) == 1 {
		return nil, fmt.Errorf("%s not found: %s", listable.Type().Name, notFound)
	} else if len(notFound) > 0 {
		return nil, fmt.Errorf("%s not found: %s", listable.Type().Names, notFound)
	}
	return found, nil
}

// matchRef finds the best input ref corresponding to a returned resource.
//
// The API response order is not guaranteed to align with request key order,
// and unsuccessful entries can be omitted. Because of that, indexing by the
// request position can bind the wrong key to a result.
//
// UUID matching is preferred globally (first pass) to avoid choosing a weaker
// name match when an exact UUID match exists later in refs.
func matchRef(refs group.Refs, name, uuid string) *group.Ref {
	if uuid != "" {
		for i := range refs {
			if refs[i].UUID != "" && refs[i].UUID == uuid {
				return &refs[i]
			}
		}
	}

	if name != "" {
		for i := range refs {
			if refs[i].Name != "" && refs[i].Name == name {
				return &refs[i]
			}
		}
	}

	return nil
}

// createdResource is what a create call reported about a resource, kept so it
// can still be shown if the resource is not yet visible to a listing.
type createdResource[T any] struct {
	data  T
	metro config.Metro
}

// recoverCreated builds resources for refs that a listing could not find but
// that a create call already reported. Refs with nothing to fall back on are
// returned unchanged.
func recoverCreated[R resource.Resource, T any](
	ctx context.Context,
	refs group.Refs,
	createdData map[string]createdResource[T],
	load func(*group.Ref, T, *config.Metro, *config.Profile) (R, error),
) ([]resource.Resource, group.Refs) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, refs
	}

	var recovered []resource.Resource
	var missing group.Refs
	for _, ref := range refs {
		key := multimetro.Key(ref).String()
		created, ok := createdData[key]
		if !ok {
			missing = append(missing, ref)
			continue
		}
		result, err := load(&ref, created.data, &created.metro, profile)
		if err != nil {
			missing = append(missing, ref)
			continue
		}
		log.G(ctx).Warn().
			Str("resource", key).
			Msg("not listed yet")
		recovered = append(recovered, result)
	}
	return recovered, missing
}

type patchOp string

const (
	patchOpSet patchOp = "set"
	patchOpAdd patchOp = "add"
	patchOpDel patchOp = "del"
)

type patchReq[P ~string] struct {
	Op    patchOp
	Prop  P
	Value any
}

func patchRequests[P ~string](fields []resource.Field, specFor func(path string, op patchOp, value any) (P, any, error)) ([]patchReq[P], error) {
	var reqs []patchReq[P]
	addReq := func(op patchOp, path string, value any) error {
		if value == nil {
			return nil
		}
		prop, converted, err := specFor(path, op, value)
		if err != nil {
			return err
		}
		if converted == nil {
			return nil
		}
		// If the converted value is a map, try to merge it with an existing
		// patch for the same prop/op. This allows multiple fields to be
		// aggregated into a single patch request.
		if m, ok := converted.(map[string]any); ok {
			for i := range reqs {
				if reqs[i].Prop == prop && reqs[i].Op == op {
					if existing, ok := reqs[i].Value.(map[string]any); ok {
						maps.Copy(existing, m)
						return nil
					}
				}
			}
		}
		reqs = append(reqs, patchReq[P]{Op: op, Prop: prop, Value: converted})
		return nil
	}

	for key, field := range resource.IterFields(fields) {
		if field.Edit == nil {
			continue
		}
		path := key.String()
		if err := addReq(patchOpSet, path, field.Edit.Set); err != nil {
			return nil, err
		}
		if err := addReq(patchOpAdd, path, field.Edit.Add); err != nil {
			return nil, err
		}
		if err := addReq(patchOpDel, path, field.Edit.Del); err != nil {
			return nil, err
		}
	}
	return reqs, nil
}
