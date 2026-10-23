// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"reflect"

	"unikraft.com/cli/internal/model"
)

// FilterEditFields filters to fields that have a pending edit patch.
func FilterEditFields(fields []model.Field) []model.Field {
	return model.FilterFields(fields, func(field model.Field) model.FilterResult {
		if field.Edit == nil {
			return model.FilterPrune
		}
		if field.Edit.Set == nil && field.Edit.Add == nil && field.Edit.Del == nil {
			return model.FilterPrune
		}
		return model.FilterInclude
	})
}

// FilterCreateFields filters to fields that have a pending create patch.
func FilterCreateFields(fields []model.Field) []model.Field {
	return model.FilterFields(fields, func(field model.Field) model.FilterResult {
		if field.Create == nil {
			return model.FilterPrune
		}
		if field.Create.Set == nil && field.Create.Add == nil && field.Create.Del == nil {
			return model.FilterPrune
		}
		return model.FilterInclude
	})
}

// FilterDisplayableEditFields filters fields for visual editing display.
// Includes fields that have an Edit patch and a non-nil Value to show.
func FilterDisplayableEditFields(fields []model.Field) []model.Field {
	return model.FilterFields(fields, func(field model.Field) model.FilterResult {
		if field.Edit == nil {
			return model.FilterPrune
		}
		rv := reflect.ValueOf(field.Edit.Set)
		if !rv.IsValid() || rv.IsZero() {
			return model.FilterPrune
		}
		return model.FilterInclude
	})
}

// FilterDisplayableCreateFields filters fields for visual creation display.
// Includes all fields that have a Create patch (even with zero values, so users can fill them in).
func FilterDisplayableCreateFields(fields []model.Field) []model.Field {
	return model.FilterFields(fields, func(field model.Field) model.FilterResult {
		if field.Create == nil {
			return model.FilterPrune
		}
		if !field.Create.Required {
			return model.FilterPrune
		}
		return model.FilterInclude
	})
}
