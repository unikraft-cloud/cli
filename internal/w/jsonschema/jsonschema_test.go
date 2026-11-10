// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package jsonschema

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
)

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
	SingleTypes(schema)
	assert.Equal(t, "array", schema.Properties["instances"].Type)
	assert.Empty(t, schema.Properties["instances"].Types)
	assert.Equal(t, "string", schema.Properties["instances"].Items.Type)
	assert.Equal(t, "integer", schema.Properties["timeout"].Type)
	assert.Equal(t, []string{"integer", "string"}, schema.Properties["either"].Types)
	assert.Equal(t, "boolean", schema.Properties["plain"].Type)
}
