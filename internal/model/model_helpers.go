// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package model

import (
	"context"
	"fmt"
	"slices"

	"golang.org/x/sync/errgroup"
)

// CloneField creates a deep copy of the given Field.
func CloneField(field Field) Field {
	newField := field
	newField.Subfields = CloneFields(field.Subfields)
	return newField
}

// CloneFields creates a deep copy of the given slice of Fields.
func CloneFields(fields []Field) []Field {
	if fields == nil {
		return nil
	}
	result := make([]Field, 0, len(fields))
	for _, field := range fields {
		newField := field
		newField.Subfields = CloneFields(field.Subfields)
		result = append(result, newField)
	}
	return result
}

// DedupeFields removes duplicate fields from the given slice of Fields.
// If a field with the same Name exists multiple times, their Subfields are
// merged recursively.
func DedupeFields(fields []Field) []Field {
	seen := make(map[string]int)
	result := make([]Field, 0, len(fields))
	for _, field := range fields {
		if idx, ok := seen[field.Name]; ok {
			result[idx].Subfields = DedupeFields(append(result[idx].Subfields, field.Subfields...))
		} else {
			seen[field.Name] = len(result)
			result = append(result, field)
		}
	}
	return result
}

// MergeFields merges the src slice of Fields into the dest slice of Fields.
func MergeFields(dest []Field, src []Field) []Field {
	return append(slices.Clone(dest), src...)
}

// RemoveFields removes fields from the dest slice of Fields based on the
// remove slice of Fields. If a field with the same Name exists in both slices,
// it is removed from dest.
func RemoveFields(dest []Field, remove []Field) []Field {
	removeMap := make(map[string]*Field)
	for _, field := range remove {
		removeMap[field.Name] = &field
	}

	result := make([]Field, 0, len(dest))
	for _, destField := range dest {
		if removeField, ok := removeMap[destField.Name]; ok {
			if len(removeField.Subfields) == 0 {
				// this field matches exactly, to remove it
				continue
			}
			destField.Subfields = RemoveFields(destField.Subfields, removeField.Subfields)
			if len(destField.Subfields) == 0 && destField.Value == nil {
				// this field has no remaining children or value, to remove it
				continue
			}
		}
		result = append(result, destField)
	}

	return result
}

// FieldsToMap converts a slice of Fields into a map[string]any suitable for
// marshaling into YAML or JSON.
func FieldsToMap(fields []Field) map[string]any {
	return fieldsToMap(fields)
}

func fieldToValue(field Field) any {
	if field.Elem != nil {
		var value any
		if field.ElemMap {
			value = fieldsToMap(field.Subfields)
		} else {
			value = fieldsToSlice(field.Subfields)
		}
		return value
	}
	if len(field.Subfields) > 0 {
		return fieldsToMap(field.Subfields)
	}
	return field.Value
}

func fieldsToMap(fields []Field) map[string]any {
	result := make(map[string]any, len(fields))
	for _, field := range fields {
		result[field.Name] = fieldToValue(field)
	}
	return result
}

func fieldsToSlice(fields []Field) []any {
	result := make([]any, 0, len(fields))
	for _, field := range fields {
		result = append(result, fieldToValue(field))
	}
	return result
}

// FilterFields filters the given fields based on the provided predicate
// function f. It recursively filters subfields as well.
func FilterFields(fields []Field, f func(Field) FilterResult) []Field {
	if fields == nil {
		return nil
	}

	result := make([]Field, 0, len(fields))
	for _, field := range fields {
		ff := f(field)
		if ff == FilterExclude {
			continue
		}
		if ff == FilterInclude {
			result = append(result, field)
			continue
		}
		field.Subfields = FilterFields(field.Subfields, f)
		if ff == FilterPrune && len(field.Subfields) == 0 {
			continue
		}
		result = append(result, field)
	}
	return result
}

type FilterResult int

const (
	FilterExclude FilterResult = iota
	FilterInclude
	FilterRecurse
	FilterPrune
)

// ResolveFields resolves ValueCallbacks for fields matching the given paths.
// Results are stored directly into Field.Value, and the callback is cleared.
func ResolveFields(ctx context.Context, fields []Field, targets []FieldPath) error {
	if len(targets) == 0 {
		return nil
	}

	eg, ctx := errgroup.WithContext(ctx)
	for path, field := range IterFields(fields) {
		if field.ValueCallback == nil {
			continue
		}

		if !slices.ContainsFunc(targets, func(target FieldPath) bool {
			return path.MatchesParent(target)
		}) {
			continue
		}

		eg.Go(func() error {
			val, err := field.ValueCallback(ctx)
			if err != nil {
				return fmt.Errorf("resolving %s: %w", path, err)
			}
			field.Value = val
			field.ValueCallback = nil
			return nil
		})
	}
	return eg.Wait()
}

func ResolveAllFields(ctx context.Context, fields []Field) error {
	eg, ctx := errgroup.WithContext(ctx)
	for path, field := range IterFields(fields) {
		if field.ValueCallback == nil {
			continue
		}
		eg.Go(func() error {
			val, err := field.ValueCallback(ctx)
			if err != nil {
				return fmt.Errorf("resolving %s: %w", path, err)
			}
			field.Value = val
			field.ValueCallback = nil
			return nil
		})
	}
	return eg.Wait()
}
