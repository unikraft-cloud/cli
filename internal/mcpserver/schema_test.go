// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/resource"
	resourcet "unikraft.com/cli/internal/resource/testing"
)

func TestToolName(t *testing.T) {
	assert.Equal(t, "instance_list", toolName("instance", "list"))
	assert.Equal(t, "instance_template_create", toolName("instance-template", "create"))
	assert.Equal(t, "volume_template_edit", toolName("Volume Template", "edit"))
}

func TestValueSchema(t *testing.T) {
	str := &jsonschema.Schema{Type: "string"}
	orString := func(typed *jsonschema.Schema) *jsonschema.Schema {
		return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{typed, str}}
	}
	tests := []struct {
		name     string
		template any
		want     *jsonschema.Schema
	}{
		{"nil", nil, str},
		{"string", "", str},
		{"int", 0, orString(&jsonschema.Schema{Type: "integer"})},
		{"uint pointer", (*uint64)(nil), orString(&jsonschema.Schema{Type: "integer"})},
		{"float", 0.0, orString(&jsonschema.Schema{Type: "number"})},
		{"bool", false, orString(&jsonschema.Schema{Type: "boolean"})},
		{"duration", time.Duration(0), str},
		{"text unmarshaler", time.Time{}, str},
		{"strings", []string(nil), orString(&jsonschema.Schema{Type: "array", Items: str})},
		{"int pointers", []*int(nil), orString(&jsonschema.Schema{Type: "array", Items: orString(&jsonschema.Schema{Type: "integer"})})},
		{"map", map[string]string(nil), orString(&jsonschema.Schema{Type: "object", AdditionalProperties: str})},
		{"struct", struct{ A int }{}, orString(&jsonschema.Schema{Type: "object"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, valueSchema(tt.template))
		})
	}
}

func TestSingleTypes(t *testing.T) {
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"instances": {Types: []string{"null", "array"}, Items: &jsonschema.Schema{Types: []string{"null", "string"}}},
			"timeout":   {Types: []string{"null", "integer"}},
			"either":    {Types: []string{"integer", "string"}},
			"plain":     {Type: "boolean"},
		},
	}
	singleTypes(schema)
	assert.Equal(t, "array", schema.Properties["instances"].Type)
	assert.Empty(t, schema.Properties["instances"].Types)
	assert.Equal(t, "string", schema.Properties["instances"].Items.Type)
	assert.Equal(t, "integer", schema.Properties["timeout"].Type)
	assert.Equal(t, []string{"integer", "string"}, schema.Properties["either"].Types)
	assert.Equal(t, "boolean", schema.Properties["plain"].Type)
}

func TestPatchValues(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{`"a"`, []string{"a"}},
		{`7`, []string{"7"}},
		{`true`, []string{"true"}},
		{`null`, nil},
		{`["a", 2, {"k":"v"}]`, []string{"a", "2", `{"k":"v"}`}},
		{`{"k": "v"}`, []string{`{"k": "v"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, patchValues(jsontext.Value(tt.in)))
		})
	}
}

func TestPatchArgs(t *testing.T) {
	args := patchArgs(map[string]jsontext.Value{
		"b": jsontext.Value(`["x", "y"]`),
		"a": jsontext.Value(`1`),
	})
	assert.Equal(t, []map[string]string{{"a": "1"}, {"b": "x"}, {"b": "y"}}, args)
}

func TestPatchSchema(t *testing.T) {
	fields, err := resourcet.TestResource{}.Fields(t.Context())
	require.NoError(t, err)

	set, add, del := patchSchema(fields, true)
	assert.ElementsMatch(t, []string{"name", "create_only", "settings.foo", "settings.bar"}, slices.Collect(maps.Keys(set.Properties)))
	assert.Empty(t, set.Required)
	assert.Equal(t, "integer", set.Properties["settings.foo"].AnyOf[0].Type)
	assert.Equal(t, "string", set.Properties["settings.foo"].AnyOf[1].Type)
	assert.Equal(t, "Foo setting.", set.Properties["settings.foo"].Description)
	assert.Equal(t, "name", set.Properties["name"].Description)
	assert.Empty(t, add.Properties)
	assert.Empty(t, del.Properties)

	set, _, _ = patchSchema(fields, false)
	assert.ElementsMatch(t, []string{"edit_only", "settings.foo", "settings.bar"}, slices.Collect(maps.Keys(set.Properties)))
}

func TestPatchSchemaRequired(t *testing.T) {
	fields := []resource.Field{
		{Name: "metro", Create: &resource.Patch{Set: "fra", Required: true}},
		{Name: "image", Create: &resource.Patch{Set: "", Required: true}},
		{Name: "tags", Edit: &resource.Patch{Set: []string(nil), Add: []string(nil), Del: []string(nil)}},
	}

	set, _, _ := patchSchema(fields, true)
	assert.Equal(t, []string{"image"}, set.Required)
	assert.Contains(t, set.Properties["metro"].Description, "Defaults to fra.")
	assert.NotContains(t, set.Properties, "tags")

	set, add, del := patchSchema(fields, false)
	assert.Equal(t, []string{"tags"}, slices.Collect(maps.Keys(set.Properties)))
	assert.Equal(t, []string{"tags"}, slices.Collect(maps.Keys(add.Properties)))
	assert.Equal(t, []string{"tags"}, slices.Collect(maps.Keys(del.Properties)))
}

func TestFieldPaths(t *testing.T) {
	fields, err := resourcet.TestResource{}.Fields(t.Context())
	require.NoError(t, err)

	paths := fieldPaths(fields)
	assert.Subset(t, paths, []string{"name", "settings.foo", "settings.bar", "tags"})
}
