// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/x/colors"
)

type Type struct {
	Name  string
	Names string
}

type Key interface {
	fmt.Stringer
	Canonical() string
}

type Resource interface {
	Type() Type
	Key() Key
	Fields(ctx context.Context) ([]Field, error)
	Raw() any
}

type GettableResource interface {
	Resource
	Get(ctx context.Context, keys []string) ([]Resource, error)
}

type ListableResource interface {
	Resource
	List(ctx context.Context) ([]Resource, error)
}

type GettableListableResource interface {
	GettableResource
	ListableResource
}

type EditableResource interface {
	GettableResource
	Edit(ctx context.Context, key string, fields []Field) error
}

type CreatableResource interface {
	GettableResource
	Create(ctx context.Context, fields []Field) ([]Resource, error)
}

type DeletableResource interface {
	GettableResource
	Delete(ctx context.Context, keys []string) error
}

type DefaultResource interface {
	GettableResource
	Default(ctx context.Context) (Resource, error)
}

type Link interface {
	// Link returns the resource type name, the key, and whether the link is
	// strong. A strong link implies ownership (e.g. an instance owning a
	// volume). A non-strong link is a weak reference that does not imply
	// ownership (e.g. an instance referencing a shared base image).
	Link() (string, Key, bool)
}

type Field struct {
	// Name is the name of the field
	Name string `json:"name"`
	// Value contains the value of the field
	Value any `json:"value,omitempty"`
	// ValueCallback computes the field value lazily. When resolved via
	// ResolveCallbacks, the result is stored in Value and the callback is cleared.
	ValueCallback func(ctx context.Context) (any, error) `json:"-"`

	// Subfields allow defining nested structures
	Subfields []Field `json:"subfields,omitempty"`
	// Elem is used to indicate that all subfields have the same substructure
	// (e.g. for arrays)
	Elem *Field `json:"elem,omitempty"`
	// ElemMap indicates this field contains map elements, and subfields should
	// be rendered as key-value pairs.
	ElemMap bool `json:"elem_map,omitempty"`

	Links []Link `json:"links,omitempty"`

	// display settings
	Verbosity FieldVerbosity `json:"verbosity"`
	Hyperlink string         `json:"hyperlink,omitempty"`
	// KeepZero indicates the field's zero value is meaningful (e.g. "0B"
	// free space) and should not be treated as empty/omittable the way an
	// unset value normally is.
	KeepZero bool `json:"keep_zero,omitempty"`

	// settings for creating or patching resources
	Create *Patch `json:"create,omitempty"`
	Edit   *Patch `json:"edit,omitempty"`

	// Flag exposes the field as a flag on create and edit commands.
	Flag *FlagSpec `json:"-"`
}

// FlagSpec describes the flag synthesised for a field. Tag is the field's own
// struct tag, whose kong keys are forwarded to that flag verbatim.
type FlagSpec struct {
	Name string
	// File marks a flag whose value is a path, and whose contents become the
	// field's value.
	File bool
	// Arg makes the field a positional argument rather than a flag.
	Arg bool
	Tag reflect.StructTag
}

func (f Field) HasChildren() bool {
	return len(f.Subfields) > 0 || f.Elem != nil
}

func (f Field) IsEmpty() bool {
	if f.Value != nil {
		if reflect.ValueOf(f.Value).IsZero() && !f.KeepZero {
			return true
		}
		if s, ok := f.Value.(fmt.Stringer); ok && s.String() == "" {
			return true
		}
		return false
	}
	if f.ValueCallback != nil {
		return false // has a potential value
	}
	if f.Elem != nil {
		return len(f.Subfields) == 0
	}

	for _, subfield := range f.Subfields {
		if !subfield.IsEmpty() {
			return false
		}
	}
	return true
}

func (f Field) Get(name string) (Field, bool) {
	for _, subfield := range f.Subfields {
		if subfield.Name == name {
			return subfield, true
		}
	}
	return Field{}, false
}

func (f Field) MarshalJSON() ([]byte, error) {
	type linkJSON struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	links := make([]linkJSON, 0, len(f.Links))
	for _, link := range f.Links {
		if link == nil {
			continue
		}
		linkType, linkKey, _ := link.Link()
		if linkType == "" || linkKey == nil {
			continue
		}
		key := linkKey.String()
		if key == "" {
			continue
		}
		links = append(links, linkJSON{Type: linkType, Key: key})
	}

	return json.Marshal(struct {
		Name      string         `json:"name"`
		Value     any            `json:"value,omitempty"`
		Subfields []Field        `json:"subfields,omitempty"`
		Elem      *Field         `json:"elem,omitempty"`
		ElemMap   bool           `json:"elem_map,omitempty"`
		Links     []linkJSON     `json:"links,omitempty"`
		Verbosity FieldVerbosity `json:"verbosity"`
		Hyperlink string         `json:"hyperlink,omitempty"`
		Create    *Patch         `json:"create,omitempty"`
		Edit      *Patch         `json:"edit,omitempty"`
	}{
		Name:      f.Name,
		Value:     f.Value,
		Subfields: f.Subfields,
		Elem:      f.Elem,
		ElemMap:   f.ElemMap,
		Links:     links,
		Verbosity: f.Verbosity,
		Hyperlink: f.Hyperlink,
		Create:    f.Create,
		Edit:      f.Edit,
	})
}

type Patch struct {
	Set any `json:"set,omitempty"`
	Add any `json:"add,omitempty"`
	Del any `json:"del,omitempty"`

	// Required indicates whether a field must be provided when patching a resource.
	Required bool `json:"required,omitempty"`
}

// Render renders the field's value to a string. Pass value.RenderOpts{} for
// full/long detail (the default), or value.RenderOpts{Short: true} for a
// more concise representation (e.g. eliding an image digest) for types that
// implement value.Renderer.
func (f Field) Render(opts value.RenderOpts) (string, error) {
	if f.Value == nil && f.ValueCallback != nil {
		return colors.InfoFg("<lazy>"), nil
	}
	return value.Render(f.Value, opts)
}

type FieldVerbosity int

const (
	FieldVerbosityUnknown FieldVerbosity = iota
	FieldVerbosityInvisible
	FieldVerbosityHidden
	FieldVerbosityLong
	FieldVerbosityShort
	FieldVerbosityNone // do not show anything
)

func (v FieldVerbosity) String() string {
	if v < 0 {
		v = 0
	}
	switch v {
	case FieldVerbosityUnknown:
		return "unknown"
	case FieldVerbosityInvisible:
		return "invisible"
	case FieldVerbosityHidden:
		return "hidden"
	case FieldVerbosityLong:
		return "long"
	case FieldVerbosityShort:
		return "short"
	default:
		return "always"
	}
}

func (v FieldVerbosity) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, `%q`, v.String()), nil
}

// CompareFieldValues compares two field values in a type-aware manner.
// It returns -1 if a < b, 0 if a == b, and 1 if a > b.
func CompareFieldValues(a, b any) int {
	return value.Compare(a, b)
}
