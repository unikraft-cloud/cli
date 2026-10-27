// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"

	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/patch"
)

func (ts *Tools) syncPartition(run func() error) error {
	p := ts.opts.Partition
	if p == nil {
		return run()
	}
	ts.partitionMu.Lock()
	defer ts.partitionMu.Unlock()
	if err := p.Reload(); err != nil {
		return err
	}
	return errors.Join(run(), p.Save())
}

func (ts *Tools) PartitionKeys[R model.GettableModel](ctx context.Context, keys []string) ([]string, error) {
	if ts.opts.Partition == nil {
		return keys, nil
	}
	found, err := ts.partitionModels[R](ctx, keys)
	if err != nil {
		return nil, err
	}
	scoped := make([]string, 0, len(found))
	for _, m := range found {
		scoped = append(scoped, m.Key().Canonical())
	}
	return scoped, nil
}

func (ts *Tools) partitionModels[R model.GettableModel](ctx context.Context, keys []string) ([]model.Model, error) {
	var empty R
	var inside []model.Model
	for _, key := range keys {
		found, err := empty.Get(ctx, []string{key})
		if err != nil && len(found) == 0 {
			return nil, err
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%s %q not found", empty.Type().Name, key)
		}
		found = slices.DeleteFunc(found, ts.opts.Partition.Missing)
		if len(found) == 0 {
			return nil, outsidePartition(empty.Type(), key)
		}
		inside = append(inside, found...)
	}
	return inside, nil
}

func (ts *Tools) checkCreate(ctx context.Context, fields []model.Field, spec patch.PatchSpec, refs map[string]model.GettableModel) error {
	if ts.opts.Partition == nil {
		return nil
	}
	for _, path := range slices.Sorted(maps.Keys(refs)) {
		for _, key := range spec.Set[path] {
			if err := ts.checkReference(ctx, refs[path], key); err != nil {
				return err
			}
		}
	}
	return ts.checkLinks(ctx, fields, spec)
}

func (ts *Tools) checkEdit[R model.GettableModel](ctx context.Context, key string, spec patch.PatchSpec) error {
	if ts.opts.Partition == nil {
		return nil
	}
	found, err := ts.partitionModels[R](ctx, []string{key})
	if err != nil {
		return err
	}
	for _, m := range found {
		fields, err := m.Fields(ctx)
		if err != nil {
			return err
		}
		if err := ts.checkLinks(ctx, fields, spec); err != nil {
			return err
		}
	}
	return nil
}

func (ts *Tools) checkLinks(ctx context.Context, fields []model.Field, spec patch.PatchSpec) error {
	patched, err := patch.PatchedFields(ctx, fields, spec)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, field := range model.IterFields(patched) {
		for _, p := range []*model.Patch{field.Create, field.Edit} {
			if p == nil {
				continue
			}
			for link := range links(p.Set, p.Add) {
				typeName, key, strong := link.Link()
				if !strong || key == nil {
					continue
				}
				m, id := ts.partitioned(typeName), typeName+":"+key.String()
				if m == nil || seen[id] {
					continue
				}
				seen[id] = true
				if err := ts.checkReference(ctx, m, key.String()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (ts *Tools) checkReference(ctx context.Context, m model.GettableModel, key string) error {
	found, err := m.Get(ctx, []string{key})
	if _, ok := errors.AsType[group.ErrRefNotFound](err); ok {
		err = nil
	}
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(found, ts.opts.Partition.Missing) {
		return nil
	}
	if slices.ContainsFunc(found, ts.opts.Partition.Has) {
		return fmt.Errorf("%s %q also matches resources outside this server's partition; name the one inside it as metro/name", m.Type().Name, key)
	}
	return outsidePartition(m.Type(), key)
}

func (ts *Tools) partitioned(typeName string) model.GettableModel {
	for _, m := range ts.opts.Partition.Cleanup {
		if g, ok := m.(model.GettableModel); ok && m.Type().Name == typeName {
			return g
		}
	}
	return nil
}

func outsidePartition(typ model.Type, key string) error {
	return fmt.Errorf("%s %q is outside this server's partition", typ.Name, key)
}

func links(values ...any) iter.Seq[model.Link] {
	return func(yield func(model.Link) bool) {
		for _, v := range values {
			if !walkLinks(reflect.ValueOf(v), yield) {
				return
			}
		}
	}
}

func walkLinks(v reflect.Value, yield func(model.Link) bool) bool {
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return true
		}
	}
	if link, ok := v.Interface().(model.Link); ok && !yield(link) {
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
