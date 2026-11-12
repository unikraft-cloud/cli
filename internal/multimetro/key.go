// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"cmp"
	"strings"

	"github.com/google/uuid"
	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/pkg/model"
)

type Key group.Ref

const (
	MetroKeySeparator = "/"
	KeyNamePrefix     = "name:"
	KeyUUIDPrefix     = "uuid:"
)

func ParseKey(s string) Key {
	metro := ""
	key := s
	if metroPart, keyPart, ok := strings.Cut(s, MetroKeySeparator); ok {
		metro = metroPart
		key = keyPart
	}

	name, id := parseKeyValue(key)
	return Key{
		Metro:   metro,
		Name:    name,
		UUID:    id,
		Display: s,
	}
}

func parseKeyValue(key string) (name string, id string) {
	if name, ok := strings.CutPrefix(key, KeyNamePrefix); ok {
		return name, ""
	}
	if id, ok := strings.CutPrefix(key, KeyUUIDPrefix); ok {
		return "", id
	}
	if uuid.Validate(key) == nil {
		return "", key
	}
	return key, ""
}

func requiresNamePrefix(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, KeyNamePrefix) || strings.HasPrefix(name, KeyUUIDPrefix) {
		return true
	}
	return uuid.Validate(name) == nil
}

func requiresIDPrefix(id string) bool {
	if id == "" {
		return false
	}
	if strings.HasPrefix(id, KeyNamePrefix) || strings.HasPrefix(id, KeyUUIDPrefix) {
		return true
	}
	return uuid.Validate(id) != nil
}

func (k Key) Ref() group.Ref {
	return group.Ref(k)
}

func (k Key) String() string {
	if k.Display != "" {
		return k.Display
	}
	return k.Canonical()
}

func (k Key) Canonical() string {
	s := ""
	if k.Metro != "" {
		s += k.Metro + MetroKeySeparator
	}
	if k.Name != "" {
		if requiresNamePrefix(k.Name) {
			s += KeyNamePrefix
		}
		s += k.Name
	} else if k.UUID != "" {
		if requiresIDPrefix(k.UUID) {
			s += KeyUUIDPrefix
		}
		s += k.UUID
	}
	return s
}

func (k Key) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

func (k *Key) UnmarshalText(text []byte) error {
	*k = ParseKey(string(text))
	return nil
}

func (k Key) Complete(prefix string) (completions []string) {
	if prefix == "" {
		return []string{cmp.Or(k.Name, k.UUID)}
	}

	metro, rest, hasMetro := strings.Cut(prefix, MetroKeySeparator)
	if !hasMetro {
		metro = ""
	}
	if metro == k.Metro {
		metro += MetroKeySeparator
		prefix = rest
		if prefix == "" {
			completions = append(completions, metro+cmp.Or(k.Name, k.UUID))
			return completions
		}
	}

	if strings.HasPrefix(prefix, KeyNamePrefix) {
		completions = append(completions, metro+KeyNamePrefix+k.Name)
	}
	if strings.HasPrefix(prefix, KeyUUIDPrefix) {
		completions = append(completions, metro+KeyUUIDPrefix+k.UUID)
	}

	if strings.HasPrefix(k.Name, prefix) {
		completions = append(completions, metro+k.Name)
	}
	if strings.HasPrefix(k.UUID, prefix) {
		completions = append(completions, metro+k.UUID)
	}
	if !hasMetro && strings.HasPrefix(k.Metro, prefix) {
		completions = append(completions, k.Metro+MetroKeySeparator+cmp.Or(k.Name, k.UUID))
	}

	return completions
}

type Keys []Key

func ParseKeys(ss []string) Keys {
	keys := make([]Key, 0, len(ss))
	for _, s := range ss {
		keys = append(keys, ParseKey(s))
	}
	return keys
}

func (ks Keys) Refs() []group.Ref {
	refs := make([]group.Ref, 0, len(ks))
	for _, k := range ks {
		refs = append(refs, k.Ref())
	}
	return refs
}

func (ks Keys) Strings() []string {
	ss := make([]string, 0, len(ks))
	for _, k := range ks {
		ss = append(ss, k.String())
	}
	return ss
}

// ByUUID returns the keys as strings. A key with a UUID gives only its UUID, so
// that a new resource with the same name does not match.
func (ks Keys) ByUUID() []string {
	ss := make([]string, 0, len(ks))
	for _, k := range ks {
		if k.UUID != "" {
			k = Key{Metro: k.Metro, UUID: k.UUID}
		}
		ss = append(ss, k.String())
	}
	return ss
}

// NotFound converts err into a model.ErrNotFound for its refs, wrapping it.
func NotFound(err group.ErrRefNotFound) error {
	keys := make([]model.Key, len(err.Refs))
	for i, ref := range err.Refs {
		keys[i] = Key(ref)
	}
	return model.ErrNotFound{Keys: keys, Err: err}
}
