// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"

	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/patch"
)

func (s *Server) syncPartition(run func() error) error {
	p := s.opts.Partition
	if p == nil {
		return run()
	}
	s.partitionMu.Lock()
	defer s.partitionMu.Unlock()
	if err := p.Reload(); err != nil {
		return err
	}
	return errors.Join(run(), p.Save())
}

func (s *Server) partitionKeys[R resource.GettableResource](ctx context.Context, keys []string) ([]string, error) {
	if s.opts.Partition == nil {
		return keys, nil
	}
	found, err := s.partitionResources[R](ctx, keys)
	if err != nil {
		return nil, err
	}
	scoped := make([]string, 0, len(found))
	for _, r := range found {
		scoped = append(scoped, r.Key().Canonical())
	}
	return scoped, nil
}

func (s *Server) partitionResources[R resource.GettableResource](ctx context.Context, keys []string) ([]resource.Resource, error) {
	var empty R
	var inside []resource.Resource
	for _, key := range keys {
		found, err := empty.Get(ctx, []string{key})
		if err != nil && len(found) == 0 {
			return nil, err
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%s %q not found", empty.Type().Name, key)
		}
		found = slices.DeleteFunc(found, s.opts.Partition.Missing)
		if len(found) == 0 {
			return nil, outsidePartition(empty.Type(), key)
		}
		inside = append(inside, found...)
	}
	return inside, nil
}

func (s *Server) checkCreate(ctx context.Context, fields []resource.Field, spec patch.PatchSpec, refs map[string]resource.GettableResource) error {
	if s.opts.Partition == nil {
		return nil
	}
	for _, path := range slices.Sorted(maps.Keys(refs)) {
		for _, key := range spec.Set[path] {
			if err := s.checkReference(ctx, refs[path], key); err != nil {
				return err
			}
		}
	}
	return s.checkLinks(ctx, fields, spec)
}

func (s *Server) checkEdit[R resource.GettableResource](ctx context.Context, key string, spec patch.PatchSpec) error {
	if s.opts.Partition == nil {
		return nil
	}
	found, err := s.partitionResources[R](ctx, []string{key})
	if err != nil {
		return err
	}
	for _, r := range found {
		fields, err := r.Fields(ctx)
		if err != nil {
			return err
		}
		if err := s.checkLinks(ctx, fields, spec); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) checkLinks(ctx context.Context, fields []resource.Field, spec patch.PatchSpec) error {
	patched, err := patch.PatchedFields(ctx, fields, spec)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, field := range resource.IterFields(patched) {
		for _, p := range []*resource.Patch{field.Create, field.Edit} {
			if p == nil {
				continue
			}
			for link := range links(p.Set, p.Add) {
				typeName, key, strong := link.Link()
				if !strong || key == nil {
					continue
				}
				r, id := s.partitioned(typeName), typeName+":"+key.String()
				if r == nil || seen[id] {
					continue
				}
				seen[id] = true
				if err := s.checkReference(ctx, r, key.String()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Server) checkReference(ctx context.Context, r resource.GettableResource, key string) error {
	found, err := r.Get(ctx, []string{key})
	if _, ok := errors.AsType[group.ErrRefNotFound](err); ok {
		err = nil
	}
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(found, s.opts.Partition.Missing) {
		return nil
	}
	if slices.ContainsFunc(found, s.opts.Partition.Has) {
		return fmt.Errorf("%s %q also matches resources outside this server's partition; name the one inside it as metro/name", r.Type().Name, key)
	}
	return outsidePartition(r.Type(), key)
}

func (s *Server) partitioned(typeName string) resource.GettableResource {
	for _, r := range s.opts.Partition.Cleanup {
		if g, ok := r.(resource.GettableResource); ok && r.Type().Name == typeName {
			return g
		}
	}
	return nil
}

func outsidePartition(typ resource.Type, key string) error {
	return fmt.Errorf("%s %q is outside this server's partition", typ.Name, key)
}

func links(values ...any) iter.Seq[resource.Link] {
	return func(yield func(resource.Link) bool) {
		for _, v := range values {
			if !walkLinks(reflect.ValueOf(v), yield) {
				return
			}
		}
	}
}

func walkLinks(v reflect.Value, yield func(resource.Link) bool) bool {
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return true
		}
	}
	if link, ok := v.Interface().(resource.Link); ok && !yield(link) {
		return false
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return walkLinks(v.Elem(), yield)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() && !walkLinks(v.Field(i), yield) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if !walkLinks(v.Index(i), yield) {
				return false
			}
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			if !walkLinks(it.Value(), yield) {
				return false
			}
		}
	}
	return true
}
