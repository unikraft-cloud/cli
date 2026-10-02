// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/resource"
)

// createFields builds the field tree the value accessors read.
func createFields() []resource.Field {
	return []resource.Field{
		{Name: "autostart", Create: &resource.Patch{Set: true}},
		{Name: "replicas", Create: &resource.Patch{}},
		{Name: "features", Create: &resource.Patch{Add: []string{"a"}}},
		{Name: "image"},
		{Name: "resources", Create: &resource.Patch{}, Subfields: []resource.Field{
			{Name: "vcpus", Create: &resource.Patch{Set: 4}},
			{Name: "memory"},
		}},
	}
}

func TestCreateValue(t *testing.T) {
	fields := createFields()

	v, ok := CreateValue[bool](fields, "autostart")
	assert.True(t, ok)
	assert.True(t, v)

	n, ok := CreateValue[int](fields, "resources.vcpus")
	assert.True(t, ok)
	assert.Equal(t, 4, n)

	// A patch that sets nothing, a field with no patch at all and a field the
	// tree does not carry all read as unset.
	_, ok = CreateValue[int64](fields, "replicas")
	assert.False(t, ok)
	_, ok = CreateValue[string](fields, "image")
	assert.False(t, ok)
	_, ok = CreateValue[string](fields, "nothing")
	assert.False(t, ok)
	_, ok = CreateValue[[]string](fields, "features")
	assert.False(t, ok)

	// The type has to match, so a guard reading the wrong one sees no value.
	_, ok = CreateValue[int32](fields, "resources.vcpus")
	assert.False(t, ok)
}

func TestSetCreateValue(t *testing.T) {
	t.Run("replaces the value a patch already sets", func(t *testing.T) {
		fields := SetCreateValue(createFields(), "autostart", false)
		require.Len(t, fields, 5)
		v, ok := CreateValue[bool](fields, "autostart")
		assert.True(t, ok)
		assert.False(t, v)
	})

	t.Run("fills a patch that sets nothing yet", func(t *testing.T) {
		fields := SetCreateValue(createFields(), "replicas", int64(3))
		require.Len(t, fields, 5)
		v, ok := CreateValue[int64](fields, "replicas")
		assert.True(t, ok)
		assert.Equal(t, int64(3), v)
	})

	t.Run("adds a path the fields do not carry", func(t *testing.T) {
		fields := SetCreateValue(createFields(), "vsock", true)
		require.Len(t, fields, 6)
		v, ok := CreateValue[bool](fields, "vsock")
		assert.True(t, ok)
		assert.True(t, v)
	})

	t.Run("reaches a nested patch", func(t *testing.T) {
		fields := SetCreateValue(createFields(), "resources.vcpus", 8)
		require.Len(t, fields, 5)
		v, ok := CreateValue[int](fields, "resources.vcpus")
		assert.True(t, ok)
		assert.Equal(t, 8, v)
	})

	t.Run("a field with no patch is not written through", func(t *testing.T) {
		fields := SetCreateValue(createFields(), "image", "nginx")
		require.Len(t, fields, 6)
		assert.Equal(t, "image", fields[5].Name)
	})
}
