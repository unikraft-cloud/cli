// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval_test

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/eval"
)

func TestToolParams(t *testing.T) {
	tools := []*mcp.Tool{
		{
			Name:        "instance_control",
			Description: "Control instances.",
			InputSchema: map[string]any{
				"$schema": "https://json-schema.org/draft/2020-12/schema",
				"type":    "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string", "enum": []any{"start", "stop"}},
					"keys":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"set": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"resources.memory": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}},
						},
						"additionalProperties": false,
					},
				},
				"required":    []any{"action", "keys"},
				"description": "The arguments.",
			},
		},
		{
			Name:        "metro_list",
			InputSchema: map[string]any{"type": "object"},
		},
	}

	params, err := eval.ToolParams(tools)
	require.NoError(t, err)
	require.Len(t, params, 2)

	data, err := json.Marshal(params[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"name": "instance_control",
		"description": "Control instances.",
		"input_schema": {
			"type": "object",
			"properties": {
				"action": {"type": "string", "enum": ["start", "stop"]},
				"keys": {"type": "array", "items": {"type": "string"}},
				"set": {
					"type": "object",
					"properties": {"resources.memory": {"anyOf": [{"type": "integer"}, {"type": "string"}]}},
					"additionalProperties": false
				}
			},
			"required": ["action", "keys"],
			"description": "The arguments."
		}
	}`, string(data))

	data, err = json.Marshal(params[1])
	require.NoError(t, err)
	assert.JSONEq(t, `{"name": "metro_list", "input_schema": {"type": "object", "properties": {}}}`, string(data))
}

func TestToolParamsRejectsNonObjectSchema(t *testing.T) {
	_, err := eval.ToolParams([]*mcp.Tool{{Name: "broken", InputSchema: "string"}})
	require.ErrorContains(t, err, "broken: input schema is not an object")
}
