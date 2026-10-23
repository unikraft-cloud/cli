// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"slices"

	"unikraft.com/cli/internal/model"
)

// MergePatches merges patch values from src into dest fields.
// For each field in dest, if src contains a patch at the same path,
// the src patch value overrides the dest patch value.
// Fields in src that don't exist in dest are inserted at their own path.
func MergePatches(dest, src []model.Field) []model.Field {
	dest = model.CloneFields(dest)

	// Build maps of src patches by path
	srcEditPatches := make(map[string]*model.Patch)
	srcCreatePatches := make(map[string]*model.Patch)
	for key, field := range model.IterFields(src) {
		keyStr := key.String()
		if field.Edit != nil {
			srcEditPatches[keyStr] = field.Edit
		}
		if field.Create != nil {
			srcCreatePatches[keyStr] = field.Create
		}
	}

	// Track which src patches we've applied
	appliedPatches := make(map[string]bool)

	// Merge src patches into dest fields
	for key, field := range model.IterFields(dest) {
		keyStr := key.String()

		if srcPatch, ok := srcEditPatches[keyStr]; ok {
			appliedPatches[keyStr] = true
			if field.Edit != nil {
				mergePatch(field.Edit, srcPatch)
			}
		}
		if srcPatch, ok := srcCreatePatches[keyStr]; ok {
			appliedPatches[keyStr] = true
			if field.Create != nil {
				mergePatch(field.Create, srcPatch)
			}
		}
	}

	// Insert src fields that weren't in dest
	for key, field := range model.IterFields(src) {
		keyStr := key.String()
		if appliedPatches[keyStr] {
			continue
		}
		// Only insert if there's a Set value
		hasSet := (field.Edit != nil && field.Edit.Set != nil) ||
			(field.Create != nil && field.Create.Set != nil)
		if !hasSet {
			continue
		}
		dest = insertPatchAtPath(dest, key, field)
	}

	return dest
}

// insertPatchAtPath attaches src's patches to dest at path, walking into (and
// where absent, creating) the fields the path passes through. Inserting on
// name alone would move a nested field such as "service.domains" to the top
// level, where nothing downstream can match it back to its real path.
func insertPatchAtPath(dest []model.Field, path model.FieldPath, src *model.Field) []model.Field {
	if len(path) == 0 {
		return dest
	}
	fields, field := &dest, &model.Field{}
	for _, name := range path {
		i := slices.IndexFunc(*fields, func(f model.Field) bool { return f.Name == name })
		if i < 0 {
			*fields = append(*fields, model.Field{Name: name})
			i = len(*fields) - 1
		}
		field = &(*fields)[i]
		fields = &field.Subfields
	}
	field.Edit = src.Edit
	field.Create = src.Create
	return dest
}

// mergePatch merges src patch values into dest patch.
func mergePatch(dest, src *model.Patch) {
	if src.Set != nil {
		dest.Set = src.Set
	}
	if src.Add != nil {
		dest.Add = src.Add
	}
	if src.Del != nil {
		dest.Del = src.Del
	}
}
