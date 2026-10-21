// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
)

type fakeBackend struct {
	kind  string
	items []fakeItem
	calls *[]string
}

type fakeItem struct {
	Metro  string `field:"metro"`
	Name   string `field:"name,short"`
	Flavor string `field:"type,short"`

	kind string
}

func (f fakeItem) Type() resource.Type {
	return resource.Type{Name: f.kind, Names: f.kind + "s"}
}

func (f fakeItem) Key() resource.Key {
	return multimetro.Key{Metro: f.Metro, Name: f.Name}
}

func (f fakeItem) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(f)
}

func (f fakeItem) Raw() any {
	return f
}

func (b fakeBackend) Type() resource.Type {
	return resource.Type{Name: b.kind, Names: b.kind + "s"}
}

func (b fakeBackend) Key() resource.Key {
	return multimetro.Key{}
}

func (b fakeBackend) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(fakeItem{})
}

func (b fakeBackend) Raw() any {
	return nil
}

func (b fakeBackend) List(ctx context.Context) ([]resource.Resource, error) {
	var results []resource.Resource
	for _, item := range b.items {
		results = append(results, item)
	}
	return results, nil
}

func (b fakeBackend) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	var results []resource.Resource
	var notFound group.Refs
	for _, key := range keys {
		k := multimetro.ParseKey(key)
		found := false
		for _, item := range b.items {
			if (k.Metro == "" || k.Metro == item.Metro) && k.Name == item.Name {
				results = append(results, item)
				found = true
			}
		}
		if !found {
			ref := k.Ref()
			ref.Display = ""
			notFound = append(notFound, ref)
		}
	}
	if len(notFound) > 0 {
		return results, group.ErrRefNotFound{Refs: notFound}
	}
	return results, nil
}

func (b fakeBackend) Edit(ctx context.Context, key string, fields []resource.Field) error {
	*b.calls = append(*b.calls, "edit "+b.kind+" "+key)
	return nil
}

func (b fakeBackend) Delete(ctx context.Context, keys []string) error {
	for _, key := range keys {
		*b.calls = append(*b.calls, "delete "+b.kind+" "+key)
	}
	return nil
}

func withFakeBackends(t *testing.T, backends ...resource.Resource) {
	t.Helper()
	original := resourceBackends
	resourceBackends = backends
	t.Cleanup(func() { resourceBackends = original })
}

func TestAnyResourceKindDoesNotCollide(t *testing.T) {
	withFakeBackends(t, fakeBackend{kind: "instance", items: []fakeItem{
		{kind: "instance", Metro: "fra", Name: "foo", Flavor: "micro"},
	}})

	resources, err := AnyResource{}.List(t.Context())
	require.NoError(t, err)
	require.Len(t, resources, 1)

	fields, err := resources[0].Fields(t.Context())
	require.NoError(t, err)

	kinds := resource.GetFieldByPath(fields, resource.FieldPath{"kind"})
	require.Len(t, kinds, 1)
	assert.Equal(t, "instance", kinds[0].Value)

	types := resource.GetFieldByPath(fields, resource.FieldPath{"type"})
	require.Len(t, types, 1)
	assert.Equal(t, "micro", types[0].Value)
}

func TestAnyResourceKeyField(t *testing.T) {
	withFakeBackends(t, fakeBackend{kind: "instance", items: []fakeItem{
		{kind: "instance", Metro: "fra", Name: "foo"},
	}})

	resources, err := AnyResource{}.List(t.Context())
	require.NoError(t, err)
	require.Len(t, resources, 1)

	fields, err := resources[0].Fields(t.Context())
	require.NoError(t, err)

	keys := resource.GetFieldByPath(fields, resource.FieldPath{"key"})
	require.Len(t, keys, 1)
	assert.Equal(t, "fra/foo", keys[0].Value)
	assert.Equal(t, "instance:fra/foo", resources[0].Key().String())
}

func withAmbiguousBackends(t *testing.T) *[]string {
	t.Helper()
	calls := &[]string{}
	withFakeBackends(t,
		fakeBackend{kind: "instance", calls: calls, items: []fakeItem{
			{kind: "instance", Metro: "fra", Name: "foo"},
		}},
		fakeBackend{kind: "volume", calls: calls, items: []fakeItem{
			{kind: "volume", Metro: "fra", Name: "foo"},
			{kind: "volume", Metro: "dal", Name: "bar"},
		}},
	)
	return calls
}

func resourceKeys(resources []resource.Resource) []string {
	keys := make([]string, len(resources))
	for i, res := range resources {
		keys[i] = res.Key().String()
	}
	return keys
}

func TestAnyResourceGet(t *testing.T) {
	tests := []struct {
		keys    []string
		want    []string
		missing []string
	}{
		{keys: []string{"foo"}, want: []string{"instance:fra/foo", "volume:fra/foo"}},
		{keys: []string{"bar"}, want: []string{"volume:dal/bar"}},
		{keys: []string{"dal/bar"}, want: []string{"volume:dal/bar"}},
		{keys: []string{"name:bar"}, want: []string{"volume:dal/bar"}},
		{keys: []string{"volume:foo"}, want: []string{"volume:fra/foo"}},
		{keys: []string{"instance:foo", "bar"}, want: []string{"instance:fra/foo", "volume:dal/bar"}},
		{keys: []string{"baz", "bar"}, want: []string{"volume:dal/bar"}, missing: []string{"baz"}},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.keys, ","), func(t *testing.T) {
			withAmbiguousBackends(t)

			resources, err := AnyResource{}.Get(t.Context(), tt.keys)
			assert.Equal(t, tt.want, resourceKeys(resources))
			if tt.missing == nil {
				require.NoError(t, err)
				return
			}
			notFound, ok := errors.AsType[group.ErrRefNotFound](err)
			require.True(t, ok, "expected ErrRefNotFound, got %v", err)
			assert.Equal(t, tt.missing, notFound.Refs.Strings())
		})
	}
}

func TestAnyResourceEdit(t *testing.T) {
	calls := withAmbiguousBackends(t)

	require.NoError(t, AnyResource{}.Edit(t.Context(), "bar", nil))
	require.NoError(t, AnyResource{}.Edit(t.Context(), "instance:foo", nil))
	assert.Equal(t, []string{"edit volume dal/bar", "edit instance foo"}, *calls)

	err := AnyResource{}.Edit(t.Context(), "foo", nil)
	require.ErrorContains(t, err, "ambiguous")
}

func TestAnyResourceDelete(t *testing.T) {
	calls := withAmbiguousBackends(t)

	require.NoError(t, AnyResource{}.Delete(t.Context(), []string{"foo", "bar"}))
	assert.Equal(t, []string{
		"delete instance fra/foo",
		"delete volume fra/foo",
		"delete volume dal/bar",
	}, *calls)
}
