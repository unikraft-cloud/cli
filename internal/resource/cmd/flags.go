// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"encoding"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/ettle/strcase"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/patch"
)

// FlagBuilder is implemented by commands whose flags come from a resource's
// field schema rather than their own struct.
type FlagBuilder interface {
	BuildFlags(opts ...kong.Option) (*FlagSet, error)
}

// AttachGeneratedFlags gives every command in the model that derives flags
// from a resource schema the flags for that schema.
func AttachGeneratedFlags(k *kong.Kong, opts ...kong.Option) error {
	return kong.Visit(k.Model.Node, func(node kong.Visitable, next kong.Next) error {
		n, ok := node.(*kong.Node)
		if !ok || !n.Target.IsValid() || !n.Target.CanAddr() {
			return next(nil)
		}
		builder, ok := reflect.TypeAssert[FlagBuilder](n.Target.Addr())
		if !ok {
			return next(nil)
		}
		set, err := builder.BuildFlags(opts...)
		if err != nil {
			return fmt.Errorf("%s: %w", n.Path(), err)
		}
		// Generated flags come first, so hand-written ones read as additions.
		n.Flags = append(set.Flags, n.Flags...)
		n.Positional = append(n.Positional, set.Args...)
		return next(nil)
	})
}

// GenerateFlags synthesises a flag for every field in fields carrying a
// `flag:"<name>"` tag and settable in the requested mode.
//
// A runtime struct is handed to a throwaway kong parser, which does the tag
// handling, mapper selection and help formatting. opts must mirror the real
// parser's, so mappers and groups resolve the same way.
func GenerateFlags(fields []resource.Field, create bool, group string, opts ...kong.Option) (*FlagSet, error) {
	var structFields []reflect.StructField
	var slots []flagSlot
	seen := make(map[string]bool)

	for key, field := range resource.IterFields(fields) {
		if field.Flag == nil {
			continue
		}
		template := patchTemplate(*field, create)
		if template == nil {
			continue
		}
		name := field.Flag.Name
		if seen[name] {
			return nil, fmt.Errorf("duplicate generated flag --%s", name)
		}
		seen[name] = true

		typ, addr := flagType(reflect.TypeOf(template))
		if field.Flag.File {
			if typ.Kind() != reflect.String {
				return nil, fmt.Errorf("flag-file %q needs a string field, got %s", name, typ)
			}
			addr = false
		}

		structFields = append(structFields, reflect.StructField{
			Name: goName(key.String()),
			Type: typ,
			Tag:  flagTag(name, group, field.Flag),
		})
		slots = append(slots, flagSlot{
			path: key.String(),
			arg:  field.Flag.Arg,
			addr: addr,
			file: field.Flag.File,
		})
	}
	if len(structFields) == 0 {
		return &FlagSet{}, nil
	}

	holder := reflect.New(reflect.StructOf(structFields))
	donor, err := kong.New(holder.Interface(), opts...)
	if err != nil {
		return nil, fmt.Errorf("generating flags: %w", err)
	}

	set := &FlagSet{Args: donor.Model.Positional}
	for _, flag := range donor.Model.Flags {
		if flag.Name == "help" {
			continue
		}
		set.Flags = append(set.Flags, flag)
	}
	if len(set.Flags)+len(set.Args) != len(slots) {
		return nil, fmt.Errorf("generated %d values for %d fields", len(set.Flags)+len(set.Args), len(slots))
	}
	// Kong splits the struct into flags and positionals, each keeping their
	// declaration order, so walk the two in step with the slots.
	var flags, args int
	for i := range slots {
		if slots[i].arg {
			slots[i].value = set.Args[args]
			args++
		} else {
			slots[i].value = set.Flags[flags].Value
			flags++
		}
		slots[i].target = holder.Elem().Field(i)
	}
	set.slots = slots
	return set, nil
}

// FlagSet is a group of flags synthesised from a resource's field schema,
// paired with the typed slots kong parses them into.
type FlagSet struct {
	Flags []*kong.Flag
	Args  []*kong.Value

	slots []flagSlot
}

type flagSlot struct {
	path   string
	target reflect.Value
	value  *kong.Value
	arg    bool
	// addr marks a []T flag standing in for a []*T field. See flagType.
	addr bool
	// file marks a flag holding a path whose contents are the value.
	file bool
}

// Apply copies every explicitly-set flag's value into spec, typed.
func (fs *FlagSet) Apply(spec *patch.PatchSpec) error {
	if fs == nil {
		return nil
	}
	for _, slot := range fs.slots {
		if slot.value == nil || !slot.value.Set {
			continue
		}
		v, err := slot.read()
		if err != nil {
			return fmt.Errorf("%s: %w", slot.value.Summary(), err)
		}
		if spec.SetTyped == nil {
			spec.SetTyped = make(map[string]any)
		}
		spec.SetTyped[slot.path] = v
	}
	return nil
}

// IsSet reports whether the flag for the given field path was given.
func (fs *FlagSet) IsSet(path string) bool {
	if fs == nil {
		return false
	}
	for _, slot := range fs.slots {
		if slot.path == path {
			return slot.value != nil && slot.value.Set
		}
	}
	return false
}

func (s flagSlot) read() (any, error) {
	if s.file {
		data, err := os.ReadFile(s.target.String())
		if err != nil {
			return nil, err
		}
		return reflect.ValueOf(strings.TrimSpace(string(data))).Convert(s.target.Type()).Interface(), nil
	}
	if !s.addr {
		return s.target.Interface(), nil
	}
	out := reflect.MakeSlice(reflect.SliceOf(reflect.PointerTo(s.target.Type().Elem())), 0, s.target.Len())
	for i := range s.target.Len() {
		elem := reflect.New(s.target.Type().Elem())
		elem.Elem().Set(s.target.Index(i))
		out = reflect.Append(out, elem)
	}
	return out.Interface(), nil
}

func patchTemplate(field resource.Field, create bool) any {
	if create {
		if field.Create == nil {
			return nil
		}
		return field.Create.Set
	}
	if field.Edit == nil {
		return nil
	}
	return field.Edit.Set
}

// flagType maps a field's type onto one kong can decode into.
//
// HACK: a []*T whose element unmarshals from text is synthesised as []T and
// converted back in flagSlot.read. kong's sliceDecoder builds each element
// with reflect.New(elem).Elem(), handing textUnmarshalerAdapter a nil *T to
// call UnmarshalText on, which segfaults. Drop this once kong allocates
// pointer elements before decoding into them.
func flagType(typ reflect.Type) (reflect.Type, bool) {
	if typ.Kind() != reflect.Slice || typ.Elem().Kind() != reflect.Pointer {
		return typ, false
	}
	if !typ.Elem().Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) {
		return typ, false
	}
	return reflect.SliceOf(typ.Elem().Elem()), true
}

// flagTag prepends the generated name and group to the field's own tag,
// carrying its kong keys through. StructTag.Get takes the first match, so the
// prepended keys win.
func flagTag(name, group string, spec *resource.FlagSpec) reflect.StructTag {
	head := fmt.Sprintf("name:%q group:%q ", name, group)
	if spec.Arg {
		// Positionals carry no group, and stay optional so --set still works.
		head = fmt.Sprintf("name:%q arg:\"\" optional:\"\" ", name)
	}
	return reflect.StructTag(head + string(spec.Tag))
}

func goName(path string) string {
	return strcase.ToPascal(strings.NewReplacer(".", " ", "-", " ").Replace(path))
}
