// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"encoding/json/jsontext"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"

	"unikraft.com/cli/internal/resource"
)

func TestTypeSchema(t *testing.T) {
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
			assert.Equal(t, tt.want, typeSchema(reflect.TypeOf(tt.template)))
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

func TestPatchSchemaRequired(t *testing.T) {
	fields := []resource.Field{
		{Name: "metro", Create: &resource.Patch{Set: "fra", Required: true}},
		{Name: "image", Create: &resource.Patch{Set: "", Required: true}},
		{Name: "tags", Edit: &resource.Patch{Set: []string(nil), Add: []string(nil), Del: []string(nil)}},
		{Name: "notes", Help: "Notes.", Edit: &resource.Patch{Add: []string(nil)}},
	}

	set, _, _ := patchSchema(fields, true)
	assert.Equal(t, []string{"image"}, set.Required)
	assert.Contains(t, set.Properties["metro"].Description, "Defaults to fra.")
	assert.NotContains(t, set.Properties, "tags")

	set, add, del := patchSchema(fields, false)
	assert.Equal(t, []string{"tags"}, slices.Collect(maps.Keys(set.Properties)))
	assert.ElementsMatch(t, []string{"tags", "notes"}, slices.Collect(maps.Keys(add.Properties)))
	assert.Equal(t, []string{"tags"}, slices.Collect(maps.Keys(del.Properties)))
	assert.Equal(t, "tags (array)", set.Properties["tags"].Description)
	assert.Equal(t, "Entries to append to tags; described under set.", add.Properties["tags"].Description)
	assert.Equal(t, "Notes.", add.Properties["notes"].Description)
}

func TestExampleValues(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, exampleValues("a,b"))
	assert.Equal(t, []string{"name=x,y", "z"}, exampleValues(`name=x\,y,z`))
	assert.Equal(t, []string{"a"}, exampleValues(" a , "))
	assert.Empty(t, exampleValues(""))
}

func TestExampleSet(t *testing.T) {
	fields := []resource.Field{
		{Name: "image", Help: "Image.", Example: "nginx:latest,my-app:v1", Create: &resource.Patch{Set: ""}},
		{Name: "ports", Example: "443:8080/http+tls", Create: &resource.Patch{Set: []string(nil)}},
		{Name: "replicas", Example: "1,3", Create: &resource.Patch{Set: 0}},
		{Name: "token", Create: &resource.Patch{Set: "", Required: true}},
		{Name: "zeta", Example: "z", Create: &resource.Patch{Set: ""}},
		{Name: "alpha", Example: "a", Create: &resource.Patch{Set: ""}},
	}
	set, _, _ := patchSchema(fields, true)
	//nolint:testifylint // the key order is part of the contract, which JSONEq would ignore
	assert.Equal(t, `{"token": "<token>", "image": "nginx:latest", "alpha": "a", "ports": ["443:8080/http+tls"]}`, exampleSet(fields, set))
	assert.Equal(t, `Image. For example "nginx:latest" or "my-app:v1".`, set.Properties["image"].Description)
	assert.Equal(t, `replicas (integer) For example "1" or "3".`, set.Properties["replicas"].Description)

	plain := []resource.Field{{Name: "x", Create: &resource.Patch{Set: ""}}}
	set, _, _ = patchSchema(plain, true)
	assert.Empty(t, exampleSet(plain, set))

	numeric := []resource.Field{{Name: "replicas", Example: "3", Create: &resource.Patch{Set: 0}}}
	set, _, _ = patchSchema(numeric, true)
	assert.Equal(t, `{"replicas": 3}`, exampleSet(numeric, set))
}

func TestFieldGlossary(t *testing.T) {
	fields := []resource.Field{
		{Name: "name", Help: "Instance name.", Create: &resource.Patch{Set: ""}},
		{Name: "state", Help: "Lifecycle state:\n  running or stopped.", Edit: &resource.Patch{Set: ""}},
		{Name: "uuid"},
		{Name: "timestamps", Subfields: []resource.Field{{Name: "created", Help: "Creation time."}}},
	}
	assert.Equal(t, " Field meanings: state: Lifecycle state: running or stopped; timestamps.created: Creation time.", fieldGlossary(fields))
	assert.Empty(t, fieldGlossary([]resource.Field{{Name: "uuid"}}))
}
