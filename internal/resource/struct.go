// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package resource

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ettle/strcase"
	"unikraft.com/cli/internal/xsync"
)

// FieldsFromStruct is a helper that converts a struct into a slice of Fields
// based on the `field` tags defined on the struct's fields.
func FieldsFromStruct(s any) (fields []Field, err error) {
	field, err := fieldFromStruct(nil, reflect.ValueOf(s))
	if err != nil {
		return nil, err
	}

	// Check if the struct implements LazyLoader
	if loader, ok := s.(LazyLoader); ok {
		wireLazyCallbacks(loader, field.Subfields)
	}

	return field.Subfields, nil
}

// LazyLoader is implemented by types whose field values should be loaded lazily.
type LazyLoader interface {
	Lazy(ctx context.Context) (any, error)
}

func fieldFromStruct(pf *ParsedField, v reflect.Value) (field *Field, err error) {
	s := v
	if s.Kind() == reflect.Pointer {
		if s.IsNil() {
			v2 := reflect.New(s.Type().Elem())
			s = v2
		}
		s = s.Elem()
	}
	if s.Kind() != reflect.Struct {
		return nil, nil
	}
	t := s.Type()

	if t.Name() != "" && pf != nil && !pf.Embed {
		return nil, nil
	}

	var fields []Field
	var links []Link
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldVal := s.Field(i)

		// Handle anonymous fields by embedding their subfields directly
		if field.Anonymous {
			embedded, err := fieldFromStruct(&ParsedField{Embed: true}, fieldVal)
			if err != nil {
				return nil, err
			}
			if embedded != nil {
				fields = append(fields, embedded.Subfields...)
				links = append(links, embedded.Links...)
			}
			if link, ok := fieldVal.Interface().(Link); ok {
				if t, k, _ := link.Link(); t != "" && k != nil && k.String() != "" {
					links = append(links, link)
				}
			}
			continue
		}

		parsedField, err := ParseField(field, fieldVal)
		if err != nil {
			return nil, err
		}
		if parsedField == nil {
			continue
		}

		result, err := fieldFromValue(parsedField, fieldVal)
		if err != nil {
			return nil, err
		}
		fields = append(fields, *result)
	}

	var value any
	if v, ok := v.Interface().(Valued); ok {
		value = v.Value()
	}

	verbosity := FieldVerbosity(0)
	for _, f := range fields {
		verbosity = max(verbosity, f.Verbosity)
	}

	return &Field{
		Value:     value,
		Subfields: fields,
		Verbosity: verbosity,
		Links:     links,
	}, nil
}

type Valued interface {
	// allows marking a type as a scalar, so it can be rendered in a nice
	// compacted view, instead of as multiple subfields
	Value() any
}

func fieldFromValue(pf *ParsedField, v reflect.Value) (*Field, error) {
	var createPatch, editPatch *Patch
	if pf.Create != nil {
		patch := *pf.Create
		createPatch = &patch
	}
	if pf.Edit != nil {
		patch := *pf.Edit
		editPatch = &patch
	}

	result := Field{
		Name:      pf.Name,
		Verbosity: pf.Verbosity,
		Value:     v.Interface(),
		KeepZero:  pf.KeepZero,
		Create:    createPatch,
		Edit:      editPatch,
		Flag:      pf.Flag,
	}

	newField, err := fieldFromStruct(pf, v)
	if err != nil {
		return nil, err
	}
	if newField != nil {
		result.Value = newField.Value
		result.Subfields = newField.Subfields
		result.Verbosity = cmp.Or(result.Verbosity, newField.Verbosity)
		result.Links = append(result.Links, newField.Links...)
	}

	newField, err = fieldFromSlice(pf, v)
	if err != nil {
		return nil, err
	}
	if newField != nil {
		result.Value = newField.Value
		result.Elem = newField.Elem
		result.Subfields = newField.Subfields
		result.Verbosity = cmp.Or(result.Verbosity, newField.Verbosity)
	}

	newField, err = fieldFromMap(pf, v)
	if err != nil {
		return nil, err
	}
	if newField != nil {
		result.Value = newField.Value
		result.Elem = newField.Elem
		result.Subfields = newField.Subfields
		result.ElemMap = newField.ElemMap
		result.Verbosity = cmp.Or(result.Verbosity, newField.Verbosity)
	}

	// Check if this value implements the Link interface
	// This is the ONE place where we check for links on field values
	if link, ok := v.Interface().(Link); ok {
		if t, k, _ := link.Link(); t != "" && k != nil && k.String() != "" {
			result.Links = append(result.Links, link)
		}
	}

	if pf.Valueless {
		result.Value = nil
	}

	result.Verbosity = cmp.Or(result.Verbosity, FieldVerbosityHidden)
	return &result, nil
}

func fieldFromSlice(pf *ParsedField, v reflect.Value) (field *Field, err error) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v = reflect.New(v.Type().Elem())
		} else {
			v = v.Elem()
		}
	}
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return nil, nil
	}

	elemType := v.Type().Elem()
	elemVal := reflect.New(elemType).Elem()
	elem, err := fieldFromStruct(pf, elemVal)
	if err != nil {
		return nil, err
	}
	if elem == nil {
		return nil, nil
	}

	var fields []Field
	for i := range v.Len() {
		vv := v.Index(i)
		field, err := fieldFromStruct(pf, vv)
		if err != nil {
			return nil, err
		}
		if field == nil {
			continue
		}
		field.Name = strconv.Itoa(i)
		fields = append(fields, *field)
	}

	return &Field{
		Elem:      elem,
		Subfields: fields,
		Verbosity: elem.Verbosity,
	}, nil
}

func fieldFromMap(pf *ParsedField, v reflect.Value) (field *Field, err error) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v = reflect.New(v.Type().Elem())
		} else {
			v = v.Elem()
		}
	}
	if v.Kind() != reflect.Map {
		return nil, nil
	}
	if v.Type().Key().Kind() != reflect.String {
		return nil, nil
	}

	elemType := v.Type().Elem()
	elemVal := reflect.New(elemType).Elem()
	elem, err := fieldFromStruct(pf, elemVal)
	if err != nil {
		return nil, err
	}
	if elem == nil {
		return nil, nil
	}

	var fields []Field
	keys := v.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int {
		return cmp.Compare(a.String(), b.String())
	})
	for _, key := range keys {
		vv := v.MapIndex(key)
		field, err := fieldFromStruct(pf, vv)
		if err != nil {
			return nil, err
		}
		if field == nil {
			continue
		}
		field.Name = key.String()
		fields = append(fields, *field)
	}

	return &Field{
		ElemMap:   true,
		Elem:      elem,
		Subfields: fields,
		Verbosity: elem.Verbosity,
	}, nil
}

type ParsedField struct {
	Name      string
	Type      reflect.Type
	Verbosity FieldVerbosity

	Embed     bool
	Valueless bool
	KeepZero  bool

	Edit   *Patch
	Create *Patch

	Flag *FlagSpec
}

func ParseField(field reflect.StructField, value reflect.Value) (*ParsedField, error) {
	if !field.IsExported() {
		return nil, nil
	}
	tag := field.Tag.Get("field")
	if tag == "-" {
		return nil, nil
	}

	opts := strings.Split(tag, ",")
	name, opts := opts[0], opts[1:]
	if name == "" {
		name = field.Name
		name = strcase.ToKebab(name)
	}

	var verbosity FieldVerbosity
	switch {
	case slices.Contains(opts, "invisible"):
		verbosity = FieldVerbosityInvisible
	case slices.Contains(opts, "hidden"):
		verbosity = FieldVerbosityHidden
	case slices.Contains(opts, "short"):
		verbosity = FieldVerbosityShort
	case slices.Contains(opts, "long"):
		verbosity = FieldVerbosityLong
	}

	edit, err := parsePatch(field.Type, value, field.Tag.Get("edit"))
	if err != nil {
		return nil, err
	}
	create, err := parsePatch(field.Type, value, field.Tag.Get("create"))
	if err != nil {
		return nil, err
	}

	embed := slices.Contains(opts, "embed")
	valueless := slices.Contains(opts, "valueless")
	keepZero := slices.Contains(opts, "keepzero")

	flag, err := parseFlagSpec(field, create, edit)
	if err != nil {
		return nil, err
	}

	return &ParsedField{
		Name:      name,
		Verbosity: verbosity,
		Type:      field.Type,
		Embed:     embed,
		Valueless: valueless,
		KeepZero:  keepZero,
		Edit:      edit,
		Create:    create,
		Flag:      flag,
	}, nil
}

// parseFlagSpec reads the opt-in `flag:"<name>"` tag, `flag-file:"<name>"` for
// a flag naming a file whose contents are the value, or `flag-arg:"<name>"`
// for a positional argument. No tag, no flag.
func parseFlagSpec(field reflect.StructField, create, edit *Patch) (*FlagSpec, error) {
	name, file, arg := field.Tag.Get("flag"), false, false
	if name == "" {
		name, file = field.Tag.Get("flag-file"), true
	}
	if name == "" {
		name, file, arg = field.Tag.Get("flag-arg"), false, true
	}
	if name == "" {
		return nil, nil
	}
	if (create == nil || create.Set == nil) && (edit == nil || edit.Set == nil) {
		return nil, fmt.Errorf("field %s has flag:%q but is not settable", field.Name, name)
	}
	return &FlagSpec{Name: name, File: file, Arg: arg, Tag: field.Tag}, nil
}

func parsePatch(tp reflect.Type, val reflect.Value, tag string) (*Patch, error) {
	if tag == "" {
		return nil, nil
	}
	parts := strings.Split(tag, ",")
	patch := &Patch{}
	for _, part := range parts {
		k, v, _ := strings.Cut(part, "=")
		var err error
		switch k {
		case "set":
			patch.Set, err = parsePatchValue(tp, v, val)
		case "add":
			patch.Add, err = parsePatchEmpty(tp, v)
		case "del":
			patch.Del, err = parsePatchEmpty(tp, v)
		case "required":
			patch.Required = true
		}
		if err != nil {
			return nil, err
		}
	}
	return patch, nil
}

func parsePatchValue(tp reflect.Type, tag string, value reflect.Value) (any, error) {
	switch tag {
	case "":
		return value.Interface(), nil
	case "keys":
		if tp.Kind() == reflect.Map {
			keys := reflect.MakeSlice(reflect.SliceOf(tp.Key()), 0, value.Len())
			for _, key := range value.MapKeys() {
				keys = reflect.Append(keys, key)
			}
			return keys.Interface(), nil
		}
		return nil, fmt.Errorf("keys patch value only valid for map types")
	default:
		return nil, fmt.Errorf("unknown patch value: %s", tag)
	}
}

// parsePatchEmpty returns a typed nil value for add/del patch templates.
// This preserves type information while keeping the value nil.
func parsePatchEmpty(tp reflect.Type, tag string) (any, error) {
	switch tag {
	case "":
		return reflect.Zero(tp).Interface(), nil
	case "strings":
		if tp.Kind() != reflect.Slice {
			return nil, fmt.Errorf("strings patch value only valid for slice types")
		}
		return []string(nil), nil
	case "keys":
		if tp.Kind() != reflect.Map {
			return nil, fmt.Errorf("keys patch value only valid for map types")
		}
		sliceType := reflect.SliceOf(tp.Key())
		return reflect.Zero(sliceType).Interface(), nil
	default:
		return nil, fmt.Errorf("unknown patch value: %s", tag)
	}
}

// wireLazyCallbacks sets up ValueCallbacks on all fields for a LazyLoader.
func wireLazyCallbacks(loader LazyLoader, fields []Field) {
	loadFields := xsync.OnceCtxValues(func(ctx context.Context) ([]Field, error) {
		populated, err := loader.Lazy(ctx)
		if err != nil {
			return nil, err
		}

		populatedFields, err := fieldFromStruct(nil, reflect.ValueOf(populated))
		if err != nil {
			return nil, err
		}
		return populatedFields.Subfields, nil
	})

	wireFieldCallbacks(loadFields, fields)
}

// wireFieldCallbacks recursively sets ValueCallbacks on fields.
func wireFieldCallbacks(loadFields func(context.Context) ([]Field, error), fields []Field) {
	for i := range fields {
		field := &fields[i]
		name := field.Name

		// Set the ValueCallback for this field
		field.ValueCallback = func(ctx context.Context) (any, error) {
			clonedFields, err := loadFields(ctx)
			if err != nil {
				return nil, err
			}
			for _, cf := range clonedFields {
				if cf.Name == name {
					return cf.Value, nil
				}
			}
			return nil, nil
		}
		field.Value = nil // Clear the value so the callback is used

		// Recursively wire up subfields with a nested loader
		if len(field.Subfields) > 0 {
			nestedLoader := func(ctx context.Context) ([]Field, error) {
				clonedFields, err := loadFields(ctx)
				if err != nil {
					return nil, err
				}
				for _, cf := range clonedFields {
					if cf.Name == name {
						return cf.Subfields, nil
					}
				}
				return nil, nil
			}
			wireFieldCallbacks(nestedLoader, field.Subfields)
		}
	}
}
