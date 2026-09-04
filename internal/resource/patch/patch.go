// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"slices"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/value"
	xmaps "unikraft.com/cli/internal/x/maps"
)

type PatchSpec struct {
	Create bool

	Set map[string][]string
	Add map[string][]string
	Del map[string][]string

	// SetTyped skips parsing for values that already are the field's type.
	// It takes precedence over Set.
	SetTyped map[string]any
}

func (spec *PatchSpec) Keys() iter.Seq[string] {
	return func(yield func(string) bool) {
		for key := range spec.Set {
			if !yield(key) {
				return
			}
		}
		for key := range spec.Add {
			if !yield(key) {
				return
			}
		}
		for key := range spec.Del {
			if !yield(key) {
				return
			}
		}
		for key := range spec.SetTyped {
			if !yield(key) {
				return
			}
		}
	}
}

// PatchedFields applies the given PatchSpec to the provided fields, returning
// only the modified fields or an error if the patching process encounters
// issues.
func PatchedFields(ctx context.Context, fields []resource.Field, spec PatchSpec) ([]resource.Field, error) {
	foundFields := make(map[string]struct{})
	typedFields := make(map[string]struct{})
	setForbiddenFields := make(map[string]struct{})
	addForbiddenFields := make(map[string]struct{})
	delForbiddenFields := make(map[string]struct{})

	fields = resource.CloneFields(fields)
	for key, field := range resource.IterFields(fields) {
		keyStr := key.String()
		foundFields[keyStr] = struct{}{}

		var original *resource.Patch
		var patch **resource.Patch
		if spec.Create {
			original = field.Create
			field.Create = nil
			patch = &field.Create
		} else {
			original = field.Edit
			field.Edit = nil
			patch = &field.Edit
		}
		if original == nil {
			original = &resource.Patch{}
		}

		typed, hasTyped := spec.SetTyped[keyStr]
		strs, hasStrs := spec.Set[keyStr]

		typedFits := hasTyped && original.Set != nil &&
			reflect.TypeOf(typed) == reflect.TypeOf(original.Set)
		if hasTyped && original.Set != nil && !typedFits {
			// One path can cover several fields of different types, as when
			// a resource and the backend it wraps both have a "type". The
			// flag fits one of them; --set reaches the rest.
			log.G(ctx).Debug().
				Str("field", keyStr).
				Stringer("got", reflect.TypeOf(typed)).
				Stringer("want", reflect.TypeOf(original.Set)).
				Msg("skipping flag value that does not fit this field")
		}

		switch {
		case original.Set == nil && (hasTyped || hasStrs):
			setForbiddenFields[keyStr] = struct{}{}
		case typedFits || hasStrs:
			var set any
			if hasStrs {
				parsed, err := value.ParseNew(strs, original.Set)
				if err != nil {
					return nil, fmt.Errorf("failed to unpack set value for %s: %w", keyStr, err)
				}
				set = parsed
			}
			if typedFits {
				typedFields[keyStr] = struct{}{}
				set = mergeSet(set, typed, hasStrs)
			}
			*patch = &resource.Patch{Set: set}
		case spec.Create && original.Set != nil && !value.IsZero(original.Set):
			*patch = &resource.Patch{Set: original.Set}
		}

		if vs, ok := spec.Add[keyStr]; ok {
			if original.Add != nil {
				add, err := value.ParseNew(vs, original.Add)
				if err != nil {
					return nil, fmt.Errorf("failed to unpack add value for %s: %w", keyStr, err)
				}
				*patch = &resource.Patch{Add: add}
			} else {
				addForbiddenFields[keyStr] = struct{}{}
			}
		} else if spec.Create && original.Add != nil && !value.IsZero(original.Add) {
			*patch = &resource.Patch{Add: original.Add}
		}
		if vs, ok := spec.Del[keyStr]; ok {
			if original.Del != nil {
				del, err := value.ParseNew(vs, original.Del)
				if err != nil {
					return nil, fmt.Errorf("failed to unpack del value for %s: %w", keyStr, err)
				}
				*patch = &resource.Patch{Del: del}
			} else {
				delForbiddenFields[keyStr] = struct{}{}
			}
		} else if spec.Create && original.Del != nil && !value.IsZero(original.Del) {
			*patch = &resource.Patch{Del: original.Del}
		}
	}

	unknownFields := make([]string, 0)
	for key := range spec.Keys() {
		if _, ok := foundFields[key]; !ok {
			unknownFields = append(unknownFields, key)
		}
	}

	unfittedFields := make([]string, 0)
	for key := range spec.SetTyped {
		_, fitted := typedFields[key]
		_, found := foundFields[key]
		_, forbidden := setForbiddenFields[key]
		if !fitted && found && !forbidden {
			unfittedFields = append(unfittedFields, key)
		}
	}

	var err error
	if len(unfittedFields) > 0 {
		slices.Sort(unfittedFields)
		err = errors.Join(err, fmt.Errorf("no field of a matching type for: %v", unfittedFields))
	}
	if len(unknownFields) > 0 {
		err = errors.Join(err, fmt.Errorf("unknown fields: %v", unknownFields))
	}
	if len(setForbiddenFields) > 0 {
		err = errors.Join(err, fmt.Errorf("fields not settable: %v", xmaps.OrderedKeys(setForbiddenFields)))
	}
	if len(addForbiddenFields) > 0 {
		err = errors.Join(err, fmt.Errorf("fields not addable: %v", xmaps.OrderedKeys(addForbiddenFields)))
	}
	if len(delForbiddenFields) > 0 {
		err = errors.Join(err, fmt.Errorf("fields not deletable: %v", xmaps.OrderedKeys(delForbiddenFields)))
	}
	if err != nil {
		return nil, err
	}

	if spec.Create {
		return FilterCreateFields(fields), nil
	}
	return FilterEditFields(fields), nil
}

// mergeSet combines a value parsed from --set with one from a flag naming the
// same field. Slices concatenate and maps overlay, both with the flag's
// entries last; a scalar takes the flag's value outright.
func mergeSet(base, typed any, hasBase bool) any {
	if !hasBase {
		return typed
	}
	b, t := reflect.ValueOf(base), reflect.ValueOf(typed)
	if !b.IsValid() || b.Kind() != t.Kind() {
		return typed
	}
	switch t.Kind() {
	case reflect.Slice:
		return reflect.AppendSlice(b, t).Interface()
	case reflect.Map:
		out := reflect.MakeMap(t.Type())
		for _, k := range b.MapKeys() {
			out.SetMapIndex(k, b.MapIndex(k))
		}
		for _, k := range t.MapKeys() {
			out.SetMapIndex(k, t.MapIndex(k))
		}
		return out.Interface()
	default:
		return typed
	}
}

// ValidateRequired checks that all required fields have a value set in the patches.
// originalFields should contain the field definitions with Required flags,
// patchedFields should contain the patches with values.
func ValidateRequired(originalFields, patchedFields []resource.Field, create bool) error {
	// Build a map of which fields have patches set
	patchedSet := make(map[string]struct{})
	for key, field := range resource.IterFields(patchedFields) {
		var patch *resource.Patch
		if create {
			patch = field.Create
		} else {
			patch = field.Edit
		}
		if patch != nil && patch.Set != nil {
			patchedSet[key.String()] = struct{}{}
		}
	}

	// Check all required fields in the original schema
	unsetFields := make(map[string]struct{})
	for key, field := range resource.IterFields(originalFields) {
		var patch *resource.Patch
		if create {
			patch = field.Create
		} else {
			patch = field.Edit
		}
		if patch != nil && patch.Required {
			if _, ok := patchedSet[key.String()]; !ok {
				unsetFields[key.String()] = struct{}{}
			}
		}
	}

	if len(unsetFields) > 0 {
		return fmt.Errorf("required values: %v", xmaps.OrderedKeys(unsetFields))
	}
	return nil
}
