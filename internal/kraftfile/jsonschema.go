// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kraftfile

import (
	"strings"

	"github.com/invopop/jsonschema"
	"golang.org/x/mod/semver"
)

func (Kraftfile) JSONSchemaExtend(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	if schema.Properties == nil {
		schema.Properties = jsonschema.NewProperties()
	}

	specSchema := specVersionSchema()
	schema.Properties.Set("spec", specSchema)
	schema.Properties.Set("specification", specSchema)

	schema.OneOf = append(schema.OneOf,
		&jsonschema.Schema{
			Required: []string{"spec"},
			Not:      &jsonschema.Schema{Required: []string{"specification"}},
		},
		&jsonschema.Schema{
			Required: []string{"specification"},
			Not:      &jsonschema.Schema{Required: []string{"spec"}},
		},
	)
	schema.AnyOf = nil

	labelsSchema, ok := schema.Properties.Get("labels")
	if !ok || labelsSchema == nil {
		labelsSchema = &jsonschema.Schema{}
		schema.Properties.Set("labels", labelsSchema)
	}

	labelsObjectSchema := &jsonschema.Schema{
		Type: "object",
		PatternProperties: map[string]*jsonschema.Schema{
			"^[a-zA-Z0-9._/-]+$": {Type: "string"},
		},
		AdditionalProperties: jsonschema.FalseSchema,
	}
	labelsListSchema := &jsonschema.Schema{
		Type:  "array",
		Items: &jsonschema.Schema{Type: "string"},
	}
	applyOneOf(labelsSchema, labelsListSchema, labelsObjectSchema)

	schema.AdditionalProperties = jsonschema.TrueSchema
}

func (Command) JSONSchemaExtend(schema *jsonschema.Schema) {
	applyOneOf(schema,
		&jsonschema.Schema{Type: "string"},
		&jsonschema.Schema{
			Type:  "array",
			Items: &jsonschema.Schema{Type: "string"},
		},
	)
}

func (Map) JSONSchemaExtend(schema *jsonschema.Schema) {
	applyOneOf(schema,
		&jsonschema.Schema{
			Type:  "array",
			Items: &jsonschema.Schema{Type: "string"},
		},
		&jsonschema.Schema{
			Type: "object",
			AdditionalProperties: &jsonschema.Schema{
				AnyOf: []*jsonschema.Schema{
					{Type: "string"},
					{Type: "number"},
					{Type: "boolean"},
					{Type: "null"},
				},
			},
		},
	)
}

func (Unikraft) JSONSchemaExtend(schema *jsonschema.Schema) {
	objectSchema := *schema
	objectSchema.OneOf = nil
	objectSchema.AdditionalProperties = jsonschema.TrueSchema
	applyOneOf(schema,
		&objectSchema,
		&jsonschema.Schema{Type: "string"},
		&jsonschema.Schema{Type: "number"},
		&jsonschema.Schema{Type: "boolean"},
	)
}

func (Template) JSONSchemaExtend(schema *jsonschema.Schema) {
	objectSchema := *schema
	objectSchema.OneOf = nil
	objectSchema.AdditionalProperties = jsonschema.TrueSchema
	applyOneOf(schema,
		&objectSchema,
		&jsonschema.Schema{Type: "string"},
		&jsonschema.Schema{Type: "number"},
		&jsonschema.Schema{Type: "boolean"},
	)
}

func (Library) JSONSchemaExtend(schema *jsonschema.Schema) {
	objectSchema := *schema
	objectSchema.OneOf = nil
	objectSchema.AdditionalProperties = jsonschema.TrueSchema
	applyOneOf(schema,
		&objectSchema,
		&jsonschema.Schema{Type: "string"},
		&jsonschema.Schema{Type: "number"},
		&jsonschema.Schema{Type: "boolean"},
	)
}

func (Rootfs) JSONSchemaExtend(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	objectSchema := *schema
	objectSchema.OneOf = nil
	objectSchema.Required = []string{"source"}
	objectSchema.AdditionalProperties = jsonschema.TrueSchema
	applyOneOf(schema,
		&jsonschema.Schema{Type: "string"},
		&objectSchema,
	)
}

func (Volumes) JSONSchemaExtend(schema *jsonschema.Schema) {
	applyOneOf(schema,
		&jsonschema.Schema{Type: "string"},
		&jsonschema.Schema{
			Type:  "array",
			Items: &jsonschema.Schema{Ref: "#/$defs/Volume"},
		},
	)
}

func (Volume) JSONSchemaExtend(schema *jsonschema.Schema) {
	objectSchema := *schema
	objectSchema.OneOf = nil
	objectSchema.AdditionalProperties = jsonschema.TrueSchema
	applyOneOf(schema,
		&jsonschema.Schema{Type: "string"},
		&objectSchema,
	)
}

func (Target) JSONSchemaExtend(schema *jsonschema.Schema) {
	objectSchema := *schema
	objectSchema.OneOf = nil
	objectSchema.AdditionalProperties = jsonschema.TrueSchema
	if objectSchema.Properties == nil {
		objectSchema.Properties = jsonschema.NewProperties()
	}

	archSchema, ok := objectSchema.Properties.Get("arch")
	if !ok || archSchema == nil {
		archSchema = &jsonschema.Schema{Type: "string"}
		objectSchema.Properties.Set("arch", archSchema)
	}
	objectSchema.Properties.Set("architecture", archSchema)

	platSchema, ok := objectSchema.Properties.Get("plat")
	if !ok || platSchema == nil {
		platSchema = &jsonschema.Schema{Type: "string"}
		objectSchema.Properties.Set("plat", platSchema)
	}
	objectSchema.Properties.Set("platform", platSchema)
	applyOneOf(schema,
		&jsonschema.Schema{Type: "string"},
		&objectSchema,
	)
}

func applyOneOf(schema *jsonschema.Schema, options ...*jsonschema.Schema) {
	if schema == nil {
		return
	}
	if schema.OneOf == nil {
		schema.OneOf = make([]*jsonschema.Schema, 0, len(options))
	}
	schema.OneOf = append(schema.OneOf, options...)
	schema.Ref = ""
	schema.Type = ""
	schema.Properties = nil
	schema.PatternProperties = nil
	schema.AdditionalProperties = nil
	schema.Items = nil
	schema.Required = nil
}

func specVersionSchema() *jsonschema.Schema {
	schema := &jsonschema.Schema{Type: "string"}
	if pattern := specVersionPattern(); pattern != "" {
		schema.Pattern = pattern
	}
	return schema
}

func specVersionPattern() string {
	min := semver.MajorMinor(SpecVersionMin)
	max := semver.MajorMinor(SpecVersionMax)
	if min == max && SpecVersionMin == SpecVersionMax {
		base := strings.TrimPrefix(min, "v")
		base = strings.ReplaceAll(base, ".", "\\.")
		return "^v?" + base + "(?:\\.0)?$"
	}
	return "^v?\\d+\\.\\d+(?:\\.\\d+)?$"
}
