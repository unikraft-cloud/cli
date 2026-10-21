// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
)

type fakeBackend struct {
	kind  string
	items []fakeItem
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
