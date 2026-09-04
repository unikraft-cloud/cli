// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"

	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"

	xmaps "unikraft.com/cli/internal/x/maps"
)

// Partition represents a testing partition for resources. Resources created in the
// partition are tracked and isolated from other resources (to provide our
// testing framework a reliable clean environment).
//
// Partitions are persisted to disk as JSON files, which track resource types
// and keys that belong to the partition.
type Partition struct {
	Path    string
	Keys    map[string]map[string]struct{}
	Cleanup []Resource
}

const UnikraftPartitionEnv = "UNIKRAFT_X_PARTITION"

func LoadPartitionFromEnv(resources ...Resource) (*Partition, error) {
	path, ok := os.LookupEnv(UnikraftPartitionEnv)
	if !ok {
		return nil, nil
	}
	partition, err := LoadPartition(path, resources...)
	if err != nil {
		return nil, fmt.Errorf("failed to load partition from %s: %w", path, err)
	}
	return partition, nil
}

func LoadPartition(path string, resources ...Resource) (*Partition, error) {
	p := Partition{Path: path, Cleanup: resources}
	p.Keys = make(map[string]map[string]struct{})
	for _, r := range resources {
		if _, ok := p.Keys[r.Type().Name]; !ok {
			p.Keys[r.Type().Name] = make(map[string]struct{})
		}
	}

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return &p, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to open partition file: %w", err)
	}
	defer f.Close()

	var keys map[string][]string
	err = json.NewDecoder(f).Decode(&keys)
	if err != nil {
		return nil, fmt.Errorf("failed to decode partition file: %w", err)
	}
	for rtype, rkeys := range keys {
		if _, ok := p.Keys[rtype]; !ok {
			continue
		}
		for _, rkey := range rkeys {
			p.Keys[rtype][rkey] = struct{}{}
		}
	}

	return &p, nil
}

func (p *Partition) Save() error {
	if p == nil {
		return nil
	}
	f, err := os.Create(p.Path)
	if err != nil {
		return fmt.Errorf("failed to create partition file: %w", err)
	}
	defer f.Close()

	keys := make(map[string][]string, len(p.Keys))
	for rtype, rkeys := range p.Keys {
		keys[rtype] = xmaps.OrderedKeys(rkeys)
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	err = enc.Encode(keys)
	if err != nil {
		return fmt.Errorf("failed to encode partition file: %w", err)
	}

	return nil
}

// Teardown attempts to delete all resources tracked by the partition. Some
// resources may not be deletable, in which case they are skipped.
func (p *Partition) Teardown(ctx context.Context) (rerr error) {
	if p == nil {
		return nil
	}
	log.G(ctx).Debug().
		Str("path", p.Path).
		Msg("tearing down partition")
	for _, r := range p.Cleanup {
		name := r.Type().Name
		r, ok := r.(DeletableResource)
		if !ok {
			log.G(ctx).Debug().
				Str("resource", name).
				Msg("skipping resource cleanup as it is not deletable")
			continue
		}

		targets := xmaps.OrderedKeys(p.Keys[name])
		log.G(ctx).Debug().
			Str("resource", name).
			Strs("targets", targets).
			Msg("cleaning up resources in partition")
		if len(targets) == 0 {
			continue
		}

		err := r.Delete(ctx, targets)
		if err != nil {
			if _, ok := errors.AsType[group.ErrRefNotFound](err); !ok {
				rerr = errors.Join(rerr, fmt.Errorf("failed to delete resources for cleanup: %w", err))
			}
			continue
		}

	}
	return rerr
}

func (p *Partition) Add(ctx context.Context, r Resource) error {
	if p == nil {
		return nil
	}
	if _, ok := p.Keys[r.Type().Name]; !ok {
		return nil
	}
	visited := make(map[string]struct{})
	return p.add(ctx, r, visited)
}

func (p *Partition) add(ctx context.Context, r Resource, visited map[string]struct{}) error {
	if p == nil {
		return nil
	}
	if _, ok := p.Keys[r.Type().Name]; !ok {
		return nil
	}
	typeName := r.Type().Name
	key := r.Key().Canonical()
	visitKey := typeName + ":" + key
	if _, ok := visited[visitKey]; ok {
		return nil
	}
	visited[visitKey] = struct{}{}
	p.Keys[typeName][key] = struct{}{}

	fields, err := r.Fields(ctx)
	if err != nil {
		return fmt.Errorf("failed to get fields for resource %s: %w", r.Key(), err)
	}
	for _, field := range IterFields(fields) {
		for _, link := range field.Links {
			if link == nil {
				continue
			}
			linkType, linkKey, strong := link.Link()
			if linkType == "" || linkKey == nil {
				continue
			}
			if !strong {
				continue
			}
			key := linkKey.Canonical()
			if key == "" {
				continue
			}
			for _, r := range p.Cleanup {
				if r.Type().Name != linkType {
					continue
				}
				if keys, ok := p.Keys[linkType]; ok {
					keys[key] = struct{}{}
				}

				r, ok := r.(GettableResource)
				if !ok {
					continue
				}
				linkedResources, err := r.Get(ctx, []string{key})
				if err != nil {
					return fmt.Errorf("failed to get linked resource %s %s: %w", linkType, key, err)
				}
				for _, linkedResource := range linkedResources {
					if err := p.add(ctx, linkedResource, visited); err != nil {
						return err
					}
				}
				break
			}
		}
	}

	return nil
}

func (p *Partition) Remove(typeName string, key string) {
	if p == nil {
		return
	}
	if _, ok := p.Keys[typeName]; !ok {
		return
	}
	delete(p.Keys[typeName], key)
}

func (p *Partition) Has(r Resource) bool {
	if p == nil {
		return true
	}
	if _, ok := p.Keys[r.Type().Name]; !ok {
		return true
	}
	_, ok := p.Keys[r.Type().Name][r.Key().Canonical()]
	return ok
}

func (p *Partition) Missing(r Resource) bool {
	return !p.Has(r)
}

func (p *Partition) WrapGettable(r GettableResource) GettableResource {
	if p == nil {
		return r
	}
	return partitionedGettableResource{
		GettableResource: r,
		partition:        p,
	}
}

func (p *Partition) WrapListable(r ListableResource) ListableResource {
	if p == nil {
		return r
	}
	return partitionedListableResource{
		ListableResource: r,
		partition:        p,
	}
}

func (p *Partition) WrapEditable(r EditableResource) EditableResource {
	if p == nil {
		return r
	}
	return partitionedEditableResource{
		EditableResource: r,
		partition:        p,
	}
}

func (p *Partition) WrapCreatable(r CreatableResource) CreatableResource {
	if p == nil {
		return r
	}
	return partitionedCreatableResource{
		CreatableResource: r,
		partition:         p,
	}
}

func (p *Partition) WrapDeletable(r DeletableResource) DeletableResource {
	if p == nil {
		return r
	}
	return partitionedDeletableResource{
		DeletableResource: r,
		partition:         p,
	}
}

type partitionedGettableResource struct {
	GettableResource
	partition *Partition
}

func (r partitionedGettableResource) Get(ctx context.Context, keys []string) ([]Resource, error) {
	resources, opErr := r.GettableResource.Get(ctx, keys)
	if opErr != nil && len(resources) == 0 {
		return nil, opErr
	}
	resources = slices.DeleteFunc(resources, r.partition.Missing)
	if len(resources) == 0 {
		if opErr != nil {
			return nil, opErr
		}
		return nil, fmt.Errorf("no resources found in the partition")
	}
	return resources, opErr
}

type partitionedListableResource struct {
	ListableResource
	partition *Partition
}

func (r partitionedListableResource) List(ctx context.Context) ([]Resource, error) {
	resources, opErr := r.ListableResource.List(ctx)
	if opErr != nil && len(resources) == 0 {
		return nil, opErr
	}
	resources = slices.DeleteFunc(resources, r.partition.Missing)
	return resources, opErr
}

type partitionedEditableResource struct {
	EditableResource
	partition *Partition
}

func (r partitionedEditableResource) Get(ctx context.Context, keys []string) ([]Resource, error) {
	return partitionedGettableResource{
		GettableResource: r.EditableResource,
		partition:        r.partition,
	}.Get(ctx, keys)
}

func (r partitionedEditableResource) Edit(ctx context.Context, key string, fields []Field) error {
	if err := r.EditableResource.Edit(ctx, key, fields); err != nil {
		return err
	}
	// re-fetch, since we may have found new strongly linked dependencies (e.g.
	// by creating a certificate)
	resources, err := r.EditableResource.Get(ctx, []string{key})
	if err != nil {
		return err
	}
	for _, res := range resources {
		if err := r.partition.Add(ctx, res); err != nil {
			return err
		}
	}
	return nil
}

type partitionedCreatableResource struct {
	CreatableResource
	partition *Partition
}

func (r partitionedCreatableResource) Get(ctx context.Context, keys []string) ([]Resource, error) {
	return partitionedGettableResource{
		GettableResource: r.CreatableResource,
		partition:        r.partition,
	}.Get(ctx, keys)
}

func (r partitionedCreatableResource) Create(ctx context.Context, fields []Field) ([]Resource, error) {
	resources, opErr := r.CreatableResource.Create(ctx, fields)
	if opErr != nil && len(resources) == 0 {
		return nil, opErr
	}
	for _, res := range resources {
		if err := r.partition.Add(ctx, res); err != nil {
			return nil, err
		}
	}
	return resources, opErr
}

type partitionedDeletableResource struct {
	DeletableResource
	partition *Partition
}

func (r partitionedDeletableResource) Get(ctx context.Context, keys []string) ([]Resource, error) {
	return partitionedGettableResource{
		GettableResource: r.DeletableResource,
		partition:        r.partition,
	}.Get(ctx, keys)
}

func (r partitionedDeletableResource) Delete(ctx context.Context, keys []string) error {
	resources, getErr := r.DeletableResource.Get(ctx, keys)

	err := r.DeletableResource.Delete(ctx, keys)
	if err != nil {
		return err
	}

	typeName := r.DeletableResource.Type().Name
	for _, res := range resources {
		r.partition.Remove(typeName, res.Key().Canonical())
	}

	return getErr
}
