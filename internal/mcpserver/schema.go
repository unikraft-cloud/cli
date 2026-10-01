// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
)

func toolName(typeName, op string) string {
	return strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(typeName)) + "_" + op
}

func listDescription(typ resource.Type, fields []resource.Field) string {
	return fmt.Sprintf(
		"List %s across every metro of the current profile, or only those named in keys. Field paths usable in filter, sort and fields: %s.",
		typ.Names, strings.Join(fieldPaths(fields), ", "),
	)
}

func getDescription(typ resource.Type) string {
	return fmt.Sprintf("Get one or more %s by key, returning every field unless fields narrows them.", typ.Names)
}

func createDescription(typ resource.Type) string {
	return fmt.Sprintf(
		"Create a %s. Values in set are strings parsed like the CLI's --set flag, or JSON values of the field's type. The created %s is returned.",
		typ.Name, typ.Name,
	)
}

func editDescription(typ resource.Type) string {
	return fmt.Sprintf(
		"Edit a %s by setting fields, adding entries to list fields or deleting entries from them, like the CLI's --set, --add and --del flags. The updated %s is returned.",
		typ.Name, typ.Name,
	)
}

func deleteDescription(typ resource.Type) string {
	return fmt.Sprintf("Delete %s by key. This cannot be undone. The deleted %s are returned.", typ.Names, typ.Names)
}

func stringArray(desc string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}, Description: desc}
}

func keysSchema(typ resource.Type) *jsonschema.Schema {
	return stringArray(fmt.Sprintf("Keys of the %s: names, UUIDs or metro/name.", typ.Names))
}

func fieldsSchema() *jsonschema.Schema {
	return stringArray("Field paths to include in the output. Defaults to every field.")
}

func dryRunSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "boolean", Description: "Return the resulting patch instead of applying it."}
}

func listSchema(typ resource.Type) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"keys":   stringArray(fmt.Sprintf("Only return these %s: names, UUIDs or metro/name. Defaults to all of them.", typ.Names)),
			"filter": stringArray("Filters such as name==my-app, metro==fra or state!=running. Every filter must match."),
			"sort":   stringArray("Field paths to sort by. Prefix a path with - for descending order."),
			"fields": fieldsSchema(),
		},
	}
}

func getSchema(typ resource.Type) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"keys":   keysSchema(typ),
			"fields": fieldsSchema(),
		},
		Required: []string{"keys"},
	}
}

func createSchema(fields []resource.Field) *jsonschema.Schema {
	set, _, _ := patchSchema(fields, true)
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"set":     set,
			"dry_run": dryRunSchema(),
		},
	}
	if len(set.Required) > 0 {
		schema.Required = []string{"set"}
	}
	return schema
}

func editSchema(typ resource.Type, fields []resource.Field) *jsonschema.Schema {
	set, add, del := patchSchema(fields, false)
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"key":     {Type: "string", Description: fmt.Sprintf("The %s to edit: a name, UUID or metro/name.", typ.Name)},
			"set":     set,
			"add":     add,
			"del":     del,
			"dry_run": dryRunSchema(),
		},
		Required: []string{"key"},
	}
}

func deleteSchema(typ resource.Type) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:       "object",
		Properties: map[string]*jsonschema.Schema{"keys": keysSchema(typ)},
		Required:   []string{"keys"},
	}
}

func patchSchema(fields []resource.Field, create bool) (set, add, del *jsonschema.Schema) {
	set = &jsonschema.Schema{Type: "object", Description: "Fields to set, keyed by dotted field path."}
	add = &jsonschema.Schema{Type: "object", Description: "Entries to add to list or map fields, keyed by dotted field path."}
	del = &jsonschema.Schema{Type: "object", Description: "Entries to remove from list or map fields, keyed by dotted field path."}

	for path, field := range resource.IterFields(fields) {
		patch := field.Edit
		if create {
			patch = field.Create
		}
		if patch == nil {
			continue
		}
		key := path.String()
		if patch.Set != nil {
			addProperty(set, key, patch.Set, describe(key, field, patch))
			if patch.Required && value.IsZero(patch.Set) {
				set.Required = append(set.Required, key)
			}
		}
		if create {
			continue
		}
		if patch.Add != nil {
			addProperty(add, key, patch.Add, describe(key, field, nil))
		}
		if patch.Del != nil {
			addProperty(del, key, patch.Del, describe(key, field, nil))
		}
	}
	return set, add, del
}

func addProperty(schema *jsonschema.Schema, key string, template any, description string) {
	prop := valueSchema(template)
	prop.Description = description
	if schema.Properties == nil {
		schema.Properties = make(map[string]*jsonschema.Schema)
	}
	schema.Properties[key] = prop
}

func describe(path string, field *resource.Field, patch *resource.Patch) string {
	var parts []string
	if field.Flag != nil {
		if help := field.Flag.Tag.Get("help"); help != "" {
			parts = append(parts, help)
		}
		if example := field.Flag.Tag.Get("example"); example != "" {
			parts = append(parts, "Example: "+example+".")
		}
	}
	if len(parts) == 0 {
		parts = append(parts, path)
	}
	if patch != nil && patch.Required && !value.IsZero(patch.Set) {
		if rendered, err := value.Render(patch.Set, value.RenderOpts{Quiet: true}); err == nil && rendered != "" {
			parts = append(parts, "Defaults to "+rendered+".")
		}
	}
	return strings.Join(parts, " ")
}

var (
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	durationType        = reflect.TypeFor[time.Duration]()
)

func valueSchema(template any) *jsonschema.Schema {
	return typeSchema(reflect.TypeOf(template))
}

func typeSchema(t reflect.Type) *jsonschema.Schema {
	if t == nil {
		return &jsonschema.Schema{Type: "string"}
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == durationType || t.Implements(textUnmarshalerType) || reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return &jsonschema.Schema{Type: "string"}
	}
	switch t.Kind() {
	case reflect.String:
		return &jsonschema.Schema{Type: "string"}
	case reflect.Bool:
		return orString(&jsonschema.Schema{Type: "boolean"})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return orString(&jsonschema.Schema{Type: "integer"})
	case reflect.Float32, reflect.Float64:
		return orString(&jsonschema.Schema{Type: "number"})
	case reflect.Slice, reflect.Array:
		return orString(&jsonschema.Schema{Type: "array", Items: typeSchema(t.Elem())})
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			return orString(&jsonschema.Schema{Type: "object", AdditionalProperties: typeSchema(t.Elem())})
		}
	case reflect.Struct:
		return orString(&jsonschema.Schema{Type: "object"})
	}
	return &jsonschema.Schema{Type: "string"}
}

func orString(typed *jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{AnyOf: []*jsonschema.Schema{typed, {Type: "string"}}}
}

func singleTypes(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	if len(schema.Types) > 0 {
		types := slices.DeleteFunc(slices.Clone(schema.Types), func(t string) bool { return t == "null" })
		if len(types) == 1 {
			schema.Type, schema.Types = types[0], nil
		}
	}
	singleTypes(schema.Items)
	for _, prop := range schema.Properties {
		singleTypes(prop)
	}
}

func fieldPaths(fields []resource.Field) []string {
	selected, err := cmd.SelectFields(fields, true, resource.FieldVerbosityInvisible, nil)
	if err != nil {
		selected = fields
	}
	var paths []string
	for path := range resource.IterFields(selected) {
		paths = append(paths, path.String())
	}
	return paths
}
