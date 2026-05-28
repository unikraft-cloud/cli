// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"errors"
	"fmt"
	"iter"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/value"
	xmaps "unikraft.com/cli/internal/x/maps"
)

type PatchSpec struct {
	Create bool

	Set map[string][]string
	Add map[string][]string
	Del map[string][]string
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
	}
}

// PatchedFields applies the given PatchSpec to the provided fields, returning
// only the modified fields or an error if the patching process encounters
// issues.
func PatchedFields(fields []resource.Field, spec *PatchSpec) ([]resource.Field, error) {
	foundFields := make(map[string]struct{})
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

		if vs, ok := spec.Set[keyStr]; ok {
			if original.Set != nil {
				set, err := value.ParseNew(vs, original.Set)
				if err != nil {
					return nil, fmt.Errorf("failed to unpack set value for %s: %w", keyStr, err)
				}
				*patch = &resource.Patch{Set: set}
			} else {
				setForbiddenFields[keyStr] = struct{}{}
			}
		} else if spec.Create && original.Set != nil && !value.IsZero(original.Set) {
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

	var err error
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
