// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"fmt"

	"unikraft.com/cli/internal/model"
)

// FieldSpecAll is the special field spec value that selects all fields,
// regardless of verbosity or emptiness.
const FieldSpecAll = "all"

func SelectFields(fields []model.Field, header bool, verbosity model.FieldVerbosity, fieldSpecs []string) ([]model.Field, error) {
	var base []model.FieldPath
	var include []model.FieldPath
	var exclude []model.FieldPath
	for _, field := range fieldSpecs {
		if len(field) == 0 {
			continue
		}
		if field == FieldSpecAll {
			if base == nil {
				base = []model.FieldPath{}
			}
			continue
		}
		switch field[0] {
		case '+':
			field = field[1:]
			include = append(include, model.ParseFieldPath(field))
		case '-':
			field = field[1:]
			exclude = append(exclude, model.ParseFieldPath(field))
		default:
			base = append(base, model.ParseFieldPath(field))
		}
	}

	var missing []model.FieldPath

	result, missing := model.FilterFieldsByPath(fields, base, !header)
	if base == nil {
		result = model.FilterFields(result, func(field model.Field) model.FilterResult {
			// remove fields that are too verbose
			if field.Verbosity < verbosity {
				return model.FilterExclude
			}
			// remove empty fields
			if !header && field.IsEmpty() {
				return model.FilterExclude
			}
			return model.FilterRecurse
		})
	} else {
		result = model.FilterFields(result, func(field model.Field) model.FilterResult {
			// remove invisible fields
			if field.Verbosity == model.FieldVerbosityInvisible {
				return model.FilterExclude
			}
			return model.FilterRecurse
		})
	}

	if len(include) > 0 {
		included, includeMissing := model.FilterFieldsByPath(fields, include, !header)
		result = model.MergeFields(result, included)
		missing = append(missing, includeMissing...)
	}

	if len(exclude) > 0 {
		excluded, excludeMissing := model.FilterFieldsByPath(fields, exclude, !header)
		result = model.RemoveFields(result, excluded)
		missing = append(missing, excludeMissing...)
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("unknown fields: %v", missing)
	}

	return result, nil
}
