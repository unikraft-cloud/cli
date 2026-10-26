// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolParams converts the tools of an MCP server into tool definitions for
// the model, passing their input schemas through unchanged.
func ToolParams(tools []*mcp.Tool) ([]anthropic.ToolUnionParam, error) {
	params := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		schema, err := inputSchema(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tool.Name, err)
		}
		param := &anthropic.ToolParam{Name: tool.Name, InputSchema: schema}
		if tool.Description != "" {
			param.Description = anthropic.String(tool.Description)
		}
		params = append(params, anthropic.ToolUnionParam{OfTool: param})
	}
	return params, nil
}

func inputSchema(schema any) (anthropic.ToolInputSchemaParam, error) {
	var param anthropic.ToolInputSchemaParam
	data, err := json.Marshal(schema)
	if err != nil {
		return param, err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return param, fmt.Errorf("input schema is not an object: %w", err)
	}
	param.Properties = map[string]any{}
	if props, ok := root["properties"]; ok {
		param.Properties = props
	}
	if required, ok := root["required"].([]any); ok {
		for _, name := range required {
			if s, ok := name.(string); ok {
				param.Required = append(param.Required, s)
			}
		}
	}
	for _, key := range []string{"properties", "required", "type", "$schema"} {
		delete(root, key)
	}
	if len(root) > 0 {
		param.ExtraFields = root
	}
	return param, nil
}
