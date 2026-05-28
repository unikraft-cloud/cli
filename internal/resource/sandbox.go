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

// Sandbox represents a testing sandbox for resources. Resources created in the
// sandbox are tracked and isolated from other resources (to provide our
// testing framework a reliable clean environment).
//
// Sandboxes are persisted to disk as JSON files, which track resource types
// and keys that belong to the sandbox.
type Sandbox struct {
	Path    string
	Keys    map[string]map[string]struct{}
	Cleanup []Resource
}

const UnikraftSandboxEnv = "UNIKRAFT_X_SANDBOX"

func LoadSandboxFromEnv(resources ...Resource) (*Sandbox, error) {
	path, ok := os.LookupEnv(UnikraftSandboxEnv)
	if !ok {
		return nil, nil
	}
	sandbox, err := LoadSandbox(path, resources...)
	if err != nil {
		return nil, fmt.Errorf("failed to load sandbox from %s: %w", path, err)
	}
	return sandbox, nil
}

func LoadSandbox(path string, resources ...Resource) (*Sandbox, error) {
	s := Sandbox{Path: path, Cleanup: resources}
	s.Keys = make(map[string]map[string]struct{})
	for _, r := range resources {
		if _, ok := s.Keys[r.Type().Name]; !ok {
			s.Keys[r.Type().Name] = make(map[string]struct{})
		}
	}

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return &s, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to open sandbox file: %w", err)
	}
	defer f.Close()

	var keys map[string][]string
	err = json.NewDecoder(f).Decode(&keys)
	if err != nil {
		return nil, fmt.Errorf("failed to decode sandbox file: %w", err)
	}
	for rtype, rkeys := range keys {
		if _, ok := s.Keys[rtype]; !ok {
			continue
		}
		for _, rkey := range rkeys {
			s.Keys[rtype][rkey] = struct{}{}
		}
	}

	return &s, nil
}

func (s *Sandbox) Save() error {
	if s == nil {
		return nil
	}
	f, err := os.Create(s.Path)
	if err != nil {
		return fmt.Errorf("failed to create sandbox file: %w", err)
	}
	defer f.Close()

	keys := make(map[string][]string, len(s.Keys))
	for rtype, rkeys := range s.Keys {
		keys[rtype] = xmaps.OrderedKeys(rkeys)
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	err = enc.Encode(keys)
	if err != nil {
		return fmt.Errorf("failed to encode sandbox file: %w", err)
	}

	return nil
}

// Teardown attempts to delete all resources tracked by the sandbox. Some
// resources may not be deletable, in which case they are skipped.
func (s *Sandbox) Teardown(ctx context.Context) (rerr error) {
	if s == nil {
		return nil
	}
	log.G(ctx).Debug().
		Str("path", s.Path).
		Msg("tearing down sandbox")
	for _, r := range s.Cleanup {
		name := r.Type().Name
		r, ok := r.(DeletableResource)
		if !ok {
			log.G(ctx).Debug().
				Str("resource", name).
				Msg("skipping resource cleanup as it is not deletable")
			continue
		}

		targets := xmaps.OrderedKeys(s.Keys[name])
		log.G(ctx).Debug().
			Str("resource", name).
			Strs("targets", targets).
			Msg("cleaning up resources in sandbox")
		if len(targets) == 0 {
			continue
		}

		err := r.Delete(ctx, targets)
		if err != nil {
			var notFoundErr group.ErrRefNotFound
			if !errors.As(err, &notFoundErr) {
				rerr = errors.Join(rerr, fmt.Errorf("failed to delete resources for cleanup: %w", err))
			}
			continue
		}

	}
	return rerr
}

func (s *Sandbox) Add(ctx context.Context, r Resource) error {
	if s == nil {
		return nil
	}
	if _, ok := s.Keys[r.Type().Name]; !ok {
		return nil
	}
	visited := make(map[string]struct{})
	return s.add(ctx, r, visited)
}

func (s *Sandbox) add(ctx context.Context, r Resource, visited map[string]struct{}) error {
	if s == nil {
		return nil
	}
	if _, ok := s.Keys[r.Type().Name]; !ok {
		return nil
	}
	typeName := r.Type().Name
	key := r.Key().Canonical()
	visitKey := typeName + ":" + key
	if _, ok := visited[visitKey]; ok {
		return nil
	}
	visited[visitKey] = struct{}{}
	s.Keys[typeName][key] = struct{}{}

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
			for _, r := range s.Cleanup {
				if r.Type().Name != linkType {
					continue
				}
				if keys, ok := s.Keys[linkType]; ok {
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
					if err := s.add(ctx, linkedResource, visited); err != nil {
						return err
					}
				}
				break
			}
		}
	}

	return nil
}

func (s *Sandbox) Remove(typeName string, key string) {
	if s == nil {
		return
	}
	if _, ok := s.Keys[typeName]; !ok {
		return
	}
	delete(s.Keys[typeName], key)
}

func (s *Sandbox) Has(r Resource) bool {
	if s == nil {
		return true
	}
	if _, ok := s.Keys[r.Type().Name]; !ok {
		return true
	}
	_, ok := s.Keys[r.Type().Name][r.Key().Canonical()]
	return ok
}

func (s *Sandbox) Missing(r Resource) bool {
	return !s.Has(r)
}

func (s *Sandbox) WrapGettable(r GettableResource) GettableResource {
	if s == nil {
		return r
	}
	return sandboxedGettableResource{
		GettableResource: r,
		sandbox:          s,
	}
}

func (s *Sandbox) WrapListable(r ListableResource) ListableResource {
	if s == nil {
		return r
	}
	return sanboxedListableResource{
		ListableResource: r,
		sandbox:          s,
	}
}

func (s *Sandbox) WrapEditable(r EditableResource) MutableResource {
	return sandboxedMutableResource{
		GettableResource: r,
		setFn:            r.Edit,
		sandbox:          s,
	}
}

func (s *Sandbox) WrapAttachable(r AttachableResource) MutableResource {
	return sandboxedMutableResource{
		GettableResource: r,
		setFn:            r.Attach,
		sandbox:          s,
	}
}

func (s *Sandbox) WrapDetachable(r DetachableResource) MutableResource {
	return sandboxedMutableResource{
		GettableResource: r,
		setFn:            r.Detach,
		sandbox:          s,
	}
}

func (s *Sandbox) WrapCreatable(r CreatableResource) CreatableResource {
	if s == nil {
		return r
	}
	return sandboxedCreatableResource{
		CreatableResource: r,
		sandbox:           s,
	}
}

func (s *Sandbox) WrapDeletable(r DeletableResource) DeletableResource {
	if s == nil {
		return r
	}
	return sandboxedDeletableResource{
		DeletableResource: r,
		sandbox:           s,
	}
}

type sandboxedGettableResource struct {
	GettableResource
	sandbox *Sandbox
}

func (r sandboxedGettableResource) Get(ctx context.Context, keys []string) ([]Resource, error) {
	resources, opErr := r.GettableResource.Get(ctx, keys)
	if opErr != nil && len(resources) == 0 {
		return nil, opErr
	}

	if r.sandbox == nil {
		return resources, nil
	}

	resources = slices.DeleteFunc(resources, r.sandbox.Missing)
	if len(resources) == 0 {
		if opErr != nil {
			return nil, opErr
		}
		return nil, fmt.Errorf("no resources found in the sandbox")
	}
	return resources, opErr
}

type sanboxedListableResource struct {
	ListableResource
	sandbox *Sandbox
}

func (r sanboxedListableResource) List(ctx context.Context) ([]Resource, error) {
	resources, opErr := r.ListableResource.List(ctx)
	if opErr != nil && len(resources) == 0 {
		return nil, opErr
	}
	resources = slices.DeleteFunc(resources, r.sandbox.Missing)
	return resources, opErr
}

type sandboxedMutableResource struct {
	GettableResource
	setFn   func(ctx context.Context, key string, fields []Field) error
	sandbox *Sandbox
}

func (r sandboxedMutableResource) Get(ctx context.Context, keys []string) ([]Resource, error) {
	return sandboxedGettableResource{
		GettableResource: r.GettableResource,
		sandbox:          r.sandbox,
	}.Get(ctx, keys)
}

func (r sandboxedMutableResource) Set(ctx context.Context, key string, fields []Field) error {
	if err := r.setFn(ctx, key, fields); err != nil {
		return err
	}

	if r.sandbox == nil {
		return nil
	}

	// re-fetch, since we may have found new strongly linked dependencies (e.g.
	// by creating a certificate)
	resources, err := r.GettableResource.Get(ctx, []string{key})
	if err != nil {
		return err
	}
	for _, res := range resources {
		if err := r.sandbox.Add(ctx, res); err != nil {
			return err
		}
	}
	return nil
}

type sandboxedCreatableResource struct {
	CreatableResource
	sandbox *Sandbox
}

func (r sandboxedCreatableResource) Get(ctx context.Context, keys []string) ([]Resource, error) {
	return sandboxedGettableResource{
		GettableResource: r.CreatableResource,
		sandbox:          r.sandbox,
	}.Get(ctx, keys)
}

func (r sandboxedCreatableResource) Create(ctx context.Context, fields []Field) ([]Resource, error) {
	resources, opErr := r.CreatableResource.Create(ctx, fields)
	if opErr != nil && len(resources) == 0 {
		return nil, opErr
	}
	for _, res := range resources {
		if err := r.sandbox.Add(ctx, res); err != nil {
			return nil, err
		}
	}
	return resources, opErr
}

type sandboxedDeletableResource struct {
	DeletableResource
	sandbox *Sandbox
}

func (r sandboxedDeletableResource) Get(ctx context.Context, keys []string) ([]Resource, error) {
	return sandboxedGettableResource{
		GettableResource: r.DeletableResource,
		sandbox:          r.sandbox,
	}.Get(ctx, keys)
}

func (r sandboxedDeletableResource) Delete(ctx context.Context, keys []string) error {
	resources, getErr := r.DeletableResource.Get(ctx, keys)

	err := r.DeletableResource.Delete(ctx, keys)
	if err != nil {
		return err
	}

	typeName := r.DeletableResource.Type().Name
	for _, res := range resources {
		r.sandbox.Remove(typeName, res.Key().Canonical())
	}

	return getErr
}
