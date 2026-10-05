// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"cmp"
	"encoding"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
)

const (
	maxExampleValues = 3
	maxExampleFields = 4
)

// examplePreference orders the fields worth showing in a create example
// before any other field that carries one.
var examplePreference = []string{
	"image", "metro", "instance", "volume", "cn", "services", "service.services",
	"domains", "size", "resources.memory", "resources.vcpus",
}

func toolName(typeName, op string) string {
	return strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(typeName)) + "_" + op
}

func article(name string) string {
	if name != "" && strings.ContainsRune("aeiouAEIOU", rune(name[0])) {
		return "an"
	}
	return "a"
}

func listDescription(typ resource.Type) string {
	return fmt.Sprintf(
		"List all %s, or fetch specific ones by passing their keys. Narrow with filter and trim the output with fields; both take the field paths listed on those parameters. Returns one JSON object per %s.",
		typ.Names, typ.Name,
	)
}

func getDescription(typ resource.Type) string {
	return fmt.Sprintf("Fetch %s by key with every field. Returns one JSON object per %s.", typ.Names, typ.Name)
}

func createDescription(typ resource.Type, fields []resource.Field, set *jsonschema.Schema, defaults map[string]jsontext.Value) string {
	required := "none"
	if len(set.Required) > 0 {
		required = strings.Join(set.Required, ", ")
	}
	text := fmt.Sprintf(
		"Create %s %s. Put the values in set, keyed by field path; list fields take arrays and anything else may be given as a string. Required: %s.",
		article(typ.Name), typ.Name, required,
	)
	if example := exampleSet(fields, set); example != "" {
		text += " Example set: " + example + "."
	}
	if len(defaults) > 0 {
		parts := make([]string, 0, len(defaults))
		for _, path := range slices.Sorted(maps.Keys(defaults)) {
			parts = append(parts, path+" defaults to "+string(defaults[path]))
		}
		text += " Unless set, " + strings.Join(parts, " and ") + "."
	}
	return text + fmt.Sprintf(" Returns the created %s.", typ.Name)
}

func editDescription(typ resource.Type, note string) string {
	text := fmt.Sprintf(
		"Change an existing %s: set replaces field values, add appends to list fields and del removes entries from them, all keyed by field path. Use dry_run to preview the change.",
		typ.Name,
	)
	if note != "" {
		text += " " + note
	}
	return text + fmt.Sprintf(" Returns the updated %s.", typ.Name)
}

func deleteDescription(typ resource.Type) string {
	return fmt.Sprintf("Delete %s by key. This cannot be undone. Returns the deleted %s.", typ.Names, typ.Names)
}

func stringArray(desc string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "array", Items: &jsonschema.Schema{Type: "string"}, Description: desc}
}

func keysSchema(typ resource.Type) *jsonschema.Schema {
	return stringArray(fmt.Sprintf("Keys of the %s: names, UUIDs or metro/name.", typ.Names))
}

func dryRunSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "boolean", Description: "Return the resulting patch instead of applying it."}
}

func listSchema(typ resource.Type, fields []resource.Field) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"keys":   stringArray(fmt.Sprintf("Return only these %s, given as names, UUIDs or metro/name. Omit to list all.", typ.Names)),
			"filter": stringArray("Conditions every result must satisfy, such as name==my-app, metro==fra or state!=running. Field paths: " + strings.Join(fieldPaths(fields), ", ") + "." + fieldGlossary(fields)),
			"sort":   stringArray("Field paths to sort by, most significant first; prefix a path with - for descending order. Same paths as filter."),
			"fields": stringArray("Field paths to include in the result. Omit for every field. Same paths as filter."),
		},
	}
}

func getSchema(typ resource.Type, fields []resource.Field) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"keys":   keysSchema(typ),
			"fields": stringArray("Field paths to include in the result. Omit for every field. Available: " + strings.Join(fieldPaths(fields), ", ") + "." + fieldGlossary(fields)),
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
			"key":     {Type: "string", Description: fmt.Sprintf("The %s to change, as a name, UUID or metro/name.", typ.Name)},
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
	set = &jsonschema.Schema{Type: "object", Description: "Field values to set, keyed by field path."}
	add = &jsonschema.Schema{Type: "object", Description: "Entries to append to list or map fields, keyed by field path; the paths are described under set."}
	del = &jsonschema.Schema{Type: "object", Description: "Entries to remove from list or map fields, keyed by field path; the paths are described under set."}

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
			prop := addProperty(set, key, patch.Set)
			prop.Description = describe(key, field, prop, patch)
			if patch.Required && value.IsZero(patch.Set) {
				set.Required = append(set.Required, key)
			}
		}
		if create {
			continue
		}
		if patch.Add != nil {
			prop := addProperty(add, key, patch.Add)
			prop.Description = describe(key, field, prop, nil)
		}
		if patch.Del != nil {
			prop := addProperty(del, key, patch.Del)
			prop.Description = cmp.Or(delDescription(key, field, patch), describe(key, field, prop, nil))
		}
	}
	return set, add, del
}

func addProperty(schema *jsonschema.Schema, key string, template any) *jsonschema.Schema {
	prop := valueSchema(template)
	if schema.Properties == nil {
		schema.Properties = make(map[string]*jsonschema.Schema)
	}
	schema.Properties[key] = prop
	return prop
}

// delDescription describes a del entry whose shape differs from the field's
// own values: a name for a list of structured entries, a key for a map.
func delDescription(path string, field *resource.Field, patch *resource.Patch) string {
	delType, fieldType := reflect.TypeOf(patch.Del), reflect.TypeOf(field.Value)
	if delType == nil || delType.Kind() != reflect.Slice || delType.Elem().Kind() != reflect.String || delType == fieldType {
		return ""
	}
	switch {
	case field.ElemMap || (fieldType != nil && fieldType.Kind() == reflect.Map):
		return "Entries to remove from " + path + ", by key."
	case field.Elem != nil:
		return "Entries to remove from " + path + ", by name."
	default:
		return ""
	}
}

func describe(path string, field *resource.Field, prop *jsonschema.Schema, patch *resource.Patch) string {
	help := field.Help
	if help == "" {
		help = path + " (" + schemaType(prop) + ")"
	} else if !strings.HasSuffix(help, ".") {
		help += "."
	}
	parts := []string{help}
	if values := exampleValues(field.Example); len(values) > 0 {
		parts = append(parts, "For example "+quotedList(values[:min(len(values), maxExampleValues)])+".")
	}
	if patch != nil && patch.Required && !value.IsZero(patch.Set) {
		if rendered, err := value.Render(patch.Set, value.RenderOpts{Quiet: true}); err == nil && rendered != "" {
			parts = append(parts, "Defaults to "+rendered+".")
		}
	}
	return strings.Join(parts, " ")
}

// exampleValues splits a field's example tag into its alternatives, which
// are separated by commas with `\,` standing for a literal comma.
func exampleValues(tag string) []string {
	var values []string
	var current strings.Builder
	escaped := false
	for _, r := range tag {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == ',':
			values = appendExample(values, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	return appendExample(values, current.String())
}

func appendExample(values []string, v string) []string {
	if v = strings.TrimSpace(v); v != "" {
		values = append(values, v)
	}
	return values
}

func quotedList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = strconv.Quote(v)
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}

// exampleSet renders a set object a model could send to create, from the
// required fields and the ones that carry an example.
func exampleSet(fields []resource.Field, set *jsonschema.Schema) string {
	examples := make(map[string]string)
	for path, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		if values := exampleValues(field.Example); len(values) > 0 {
			examples[path.String()] = values[0]
		}
	}

	var paths []string
	add := func(path string) {
		if len(paths) < maxExampleFields && !slices.Contains(paths, path) && set.Properties[path] != nil {
			paths = append(paths, path)
		}
	}
	for _, path := range set.Required {
		add(path)
	}
	for _, path := range examplePreference {
		if _, ok := examples[path]; ok {
			add(path)
		}
	}
	for _, path := range slices.Sorted(maps.Keys(examples)) {
		add(path)
	}
	if len(paths) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("{")
	for i, path := range paths {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q: %s", path, exampleJSON(set.Properties[path], path, examples[path]))
	}
	b.WriteString("}")
	return b.String()
}

func exampleJSON(prop *jsonschema.Schema, path, example string) string {
	if example == "" {
		return strconv.Quote("<" + path + ">")
	}
	switch schemaType(prop) {
	case "array":
		return "[" + strconv.Quote(example) + "]"
	case "integer", "number":
		if _, err := strconv.ParseFloat(example, 64); err == nil {
			return example
		}
	}
	return strconv.Quote(example)
}

func schemaType(prop *jsonschema.Schema) string {
	if len(prop.AnyOf) > 0 {
		prop = prop.AnyOf[0]
	}
	return cmp.Or(prop.Type, "string")
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

// fieldGlossary explains the fields that carry help but are not create
// input, so a model knows what values a filter such as state==standby can
// match.
func fieldGlossary(fields []resource.Field) string {
	var entries []string
	for path, field := range resource.IterFields(fields) {
		if field.Help == "" || field.Create != nil {
			continue
		}
		entries = append(entries, path.String()+": "+strings.TrimSuffix(strings.Join(strings.Fields(field.Help), " "), "."))
	}
	if len(entries) == 0 {
		return ""
	}
	return " Field meanings: " + strings.Join(entries, "; ") + "."
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
