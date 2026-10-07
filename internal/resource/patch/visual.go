// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"bytes"
	"cmp"
	"context"
	"encoding"
	"fmt"
	"io"
	"reflect"
	"slices"

	"sigs.k8s.io/yaml"

	"unikraft.com/cli/internal/resource"
)

// EditorFunc is a callback that takes input YAML content and returns edited content.
// This allows for different editing implementations (file-based, in-memory, etc.)
type EditorFunc func(ctx context.Context, input []byte) ([]byte, error)

// Edit applies patches to fields using the provided editor function.
// It serializes fields to YAML, calls the editor, and updates patch.Set values based on changes.
func Edit(ctx context.Context, res resource.Resource, fields []resource.Field, patches []resource.Field, editor EditorFunc) ([]resource.Field, error) {
	return edit(ctx, res, fields, patches, false, editor)
}

// Create applies patches to fields for creation using the provided editor function.
// It serializes fields to YAML, calls the editor, and updates patch.Set values based on changes.
func Create(ctx context.Context, res resource.Resource, fields []resource.Field, patches []resource.Field, editor EditorFunc) ([]resource.Field, error) {
	return edit(ctx, res, fields, patches, true, editor)
}

// SaveYAML writes fields to a writer as YAML.
// If create is true, uses create patches; otherwise uses edit patches.
func SaveYAML(res resource.Resource, fields []resource.Field, patches []resource.Field, w io.Writer, create bool) error {
	data, err := saveFields(res, fields, patches, create)
	if err != nil {
		return fmt.Errorf("failed to serialize fields: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write YAML: %w", err)
	}
	return nil
}

// ContentEditorFunc creates an EditorFunc that ignores input and returns the provided content.
// This is designed to work with kong's FileContentFlag or similar pre-read file content.
func ContentEditorFunc(content []byte) EditorFunc {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		return content, nil
	}
}

// edit is the core implementation that handles both edit and create operations.
func edit(ctx context.Context, res resource.Resource, fields []resource.Field, patches []resource.Field, create bool, editor EditorFunc) ([]resource.Field, error) {
	data, err := saveFields(res, fields, patches, create)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize fields: %w", err)
	}

	editedData, err := editor(ctx, data)
	if err != nil {
		return nil, err
	}
	editedData = bytes.TrimSpace(editedData)
	if len(editedData) == 0 {
		return nil, fmt.Errorf("edited data is empty")
	}

	fields, err = loadFieldPatches(fields, editedData, create)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize edited fields: %w", err)
	}
	return fields, nil
}

// saveFields serializes fields to YAML for editing.
// It collects patch.Set values and merges in any pending patches.
func saveFields(res resource.Resource, fields []resource.Field, patches []resource.Field, create bool) ([]byte, error) {
	// Filter to displayable fields and clear Add/Del templates
	var displayableFields []resource.Field
	if create {
		displayableFields = FilterDisplayableCreateFields(fields)
	} else {
		displayableFields = FilterDisplayableEditFields(fields)
	}

	// Clear Add/Del on all fields - they come from struct tags and aren't real values
	for _, field := range resource.IterFields(displayableFields) {
		if field.Edit != nil {
			field.Edit.Add = nil
			field.Edit.Del = nil
		}
		if field.Create != nil {
			field.Create.Add = nil
			field.Create.Del = nil
		}
	}

	// Merge in pending patches - if Add/Del become non-nil, they were set via --add/--del
	displayableFields = MergePatches(displayableFields, patches)

	// Collect values from patch.Set into a map for YAML serialization
	result := make(map[string]any)
	for key, field := range resource.IterFields(displayableFields) {
		var patch *resource.Patch
		if create {
			patch = field.Create
		} else {
			patch = field.Edit
		}
		if patch == nil || patch.Set == nil {
			continue
		}
		if patch.Add != nil {
			return nil, fmt.Errorf("%s was added, but visual editing does not support this", key)
		}
		if patch.Del != nil {
			return nil, fmt.Errorf("%s was deleted, but visual editing does not support this", key)
		}
		if err := setNestedValue(result, key, normalizeValue(patch.Set)); err != nil {
			return nil, fmt.Errorf("failed to serialize field %s: %w", key, err)
		}
	}

	yamlBytes, err := yaml.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fields to YAML: %w", err)
	}

	line := fmt.Sprintf("# %s", res.Type().Name)
	if key := res.Key().String(); key != "" {
		line = line + " " + key
	}
	yamlBytes = append([]byte(line+"\n"), yamlBytes...)
	return yamlBytes, nil
}

// loadFieldPatches parses edited YAML and updates patch.Set values for changed fields.
func loadFieldPatches(fields []resource.Field, data []byte, create bool) ([]resource.Field, error) {
	fields = resource.CloneFields(fields)

	var obj map[string]any
	if err := yaml.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal edited data: %w", err)
	}

	// Process all fields that have a patch template, deepest paths first.
	// A field such as "service" and a descendant such as "service.domains"
	// can share the same YAML subtree; since each field's value is deleted
	// from obj once consumed (to detect unknown fields below), an ancestor
	// processed first would consume - and delete - the whole subtree before
	// its descendants get a chance to read their own part of it.
	//
	// This is safe only because every such ancestor reads a disjoint part of
	// its subtree (Instance.Create's "service" case takes just the name/uuid
	// link). An ancestor wanting the whole subtree would find descendant
	// values already deleted and silently patch partial data, so a new
	// nested set field must claim values its descendants do not.
	type fieldEntry struct {
		key   resource.FieldPath
		field *resource.Field
	}
	var entries []fieldEntry
	for key, field := range resource.IterFields(fields) {
		entries = append(entries, fieldEntry{key, field})
	}
	slices.SortStableFunc(entries, func(a, b fieldEntry) int {
		return cmp.Compare(len(b.key), len(a.key))
	})

	for _, entry := range entries {
		key, field := entry.key, entry.field
		var patch *resource.Patch
		if create {
			patch = field.Create
		} else {
			patch = field.Edit
		}
		if patch == nil {
			continue
		}

		// Visual editing only supports Set operations - always clear Add/Del
		patch.Add = nil
		patch.Del = nil
		if patch.Set == nil {
			continue
		}

		// Get the value from the edited YAML
		newValue, found := getNestedValue(obj, key)

		// Mark this path as consumed so we can detect unknown fields
		deleteNestedValue(obj, key)

		if !found {
			// Field not in YAML
			patch.Set = nil
			continue
		}

		// Convert newValue to the correct type (using patch.Set as type template)
		convertedValue, err := convertValue(newValue, reflect.TypeOf(patch.Set))
		if err != nil {
			return nil, fmt.Errorf("failed to convert value for field %s: %w", key, err)
		}

		// In edit mode, skip fields that haven't changed from the original value.
		// In create mode, there is no prior state, so we always keep the value.
		if !create {
			if originalValue := field.Value; originalValue != nil {
				convertedOriginal, err := convertValue(originalValue, reflect.TypeOf(patch.Set))
				if err == nil && valuesEqual(convertedOriginal, convertedValue) {
					patch.Set = nil
					continue
				}
			}
		}

		// Value differs from original (or field wasn't displayed) - keep the patch
		patch.Set = convertedValue
	}

	// Check for unknown fields
	unknownFields := collectKeys(obj, nil)
	if len(unknownFields) > 0 {
		return nil, fmt.Errorf("unknown fields: %v", unknownFields)
	}

	// Return only fields with pending patches
	if create {
		return FilterCreateFields(fields), nil
	}
	return FilterEditFields(fields), nil
}

// getNestedValue retrieves a value from a nested map structure based on a field path.
func getNestedValue(m map[string]any, path resource.FieldPath) (any, bool) {
	if len(path) == 0 {
		return nil, false
	}
	value, ok := m[path[0]]
	if !ok {
		return nil, false
	}
	if len(path) == 1 {
		return value, true
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	return getNestedValue(nested, path[1:])
}

// setNestedValue sets a value in a nested map structure based on a field path.
func setNestedValue(m map[string]any, path resource.FieldPath, value any) error {
	if len(path) == 0 {
		return nil
	}
	if len(path) == 1 {
		m[path[0]] = value
		return nil
	}
	// Create nested map if needed
	key := path[0]
	nested, ok := m[key].(map[string]any)
	if !ok {
		// An ancestor field carries its own value (e.g. "service" holding an
		// InstanceService alongside a "service.domains" field). Expand it so
		// the descendant has somewhere to land; keys the descendant sets then
		// win over the ones the ancestor's own value produced.
		expanded, err := objectFields(m[key])
		if err != nil {
			return err
		}
		nested = expanded
		m[key] = nested
	}
	return setNestedValue(nested, path[1:], value)
}

// objectFields re-expresses a value as the map its own marshaling produces.
// A value that marshals to something other than an object contributes no
// fields, so it yields an empty map; only a failure to marshal is an error.
func objectFields(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil || m == nil {
		return map[string]any{}, nil
	}
	return m, nil
}

// deleteNestedValue removes a value from a nested map structure and cleans up empty parents.
func deleteNestedValue(m map[string]any, path resource.FieldPath) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		delete(m, path[0])
		return
	}
	value, ok := m[path[0]]
	if !ok {
		return
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return
	}
	deleteNestedValue(nested, path[1:])
	// Clean up empty nested maps
	if len(nested) == 0 {
		delete(m, path[0])
	}
}

// collectKeys collects all remaining keys in a nested map as field paths.
func collectKeys(m map[string]any, prefix resource.FieldPath) []string {
	var keys []string
	for k, v := range m {
		path := append(prefix, k)
		if nested, ok := v.(map[string]any); ok {
			keys = append(keys, collectKeys(nested, path)...)
		} else {
			keys = append(keys, path.String())
		}
	}
	return keys
}

// valuesEqual compares two values for semantic equality.
func valuesEqual(a, b any) bool {
	// Fast path: try reflect.DeepEqual first
	if reflect.DeepEqual(a, b) {
		return true
	}

	// For TextMarshalers, compare by marshaled text
	aMarshaler, aOk := a.(encoding.TextMarshaler)
	bMarshaler, bOk := b.(encoding.TextMarshaler)
	if aOk && bOk {
		aText, aErr := aMarshaler.MarshalText()
		bText, bErr := bMarshaler.MarshalText()
		if aErr == nil && bErr == nil {
			return bytes.Equal(aText, bText)
		}
	}

	return false
}

// convertValue converts a value to the target type by round-tripping through YAML.
// This ensures proper handling of all YAML-supported types.
func convertValue(value any, targetType reflect.Type) (any, error) {
	if value == nil {
		return reflect.Zero(targetType).Interface(), nil
	}

	valueType := reflect.TypeOf(value)
	if valueType.AssignableTo(targetType) {
		return value, nil
	}
	if valueType.ConvertibleTo(targetType) {
		return reflect.ValueOf(value).Convert(targetType).Interface(), nil
	}

	// Round-trip through YAML for complex type conversions
	yamlBytes, err := yaml.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("cannot convert %T to %s: failed to marshal: %w", value, targetType.String(), err)
	}

	target := reflect.New(targetType).Interface()
	if err := yaml.Unmarshal(yamlBytes, target); err != nil {
		return nil, fmt.Errorf("cannot convert %T to %s: failed to unmarshal: %w", value, targetType.String(), err)
	}
	return reflect.ValueOf(target).Elem().Interface(), nil
}

// normalizeValue converts nil slices/maps to empty ones for cleaner YAML output.
// This ensures `services: []` instead of `services: null`.
func normalizeValue(v any) any {
	if v == nil {
		return v
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map:
		if rv.IsNil() {
			return reflect.MakeSlice(rv.Type(), 0, 0).Interface()
		}
	}
	return v
}
