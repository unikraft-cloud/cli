// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"encoding/json"
	"strings"

	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
)

// Link models a resource reference using group.Ref-compatible fields.
type Link[T resource.Resource] struct {
	Metro string `name:"metro" json:"metro,omitempty" mirror:"metro" field:"-"`
	Name  string `name:"name" json:"name,omitempty" mirror:"name" field:",long" help:"Name of the linked resource."`
	UUID  string `name:"uuid" json:"uuid,omitempty" mirror:"uuid" field:",long" help:"Unique identifier of the linked resource."`
}

func (l Link[T]) Ref() group.Ref {
	return group.Ref{
		Metro: l.Metro,
		Name:  l.Name,
		UUID:  l.UUID,
	}
}

func (l Link[T]) Link() (string, resource.Key, bool) {
	if l.Name == "" && l.UUID == "" {
		return "", nil, false
	}
	var zero T
	return zero.Type().Name, multimetro.Key{
		Metro: l.Metro,
		Name:  l.Name,
		UUID:  l.UUID,
	}, true
}

// FormatLink renders a link as a multimetro key string, matching ParseLink.
//
// Deliberately a plain function rather than a MarshalText method: Link is
// always embedded anonymously, so encoding.TextMarshaler would promote onto
// every embedder - including through a `type X Y` alias, which strips
// explicit methods but not promoted ones. Embed TextLink to opt in.
func FormatLink[T resource.Resource](l Link[T]) ([]byte, error) {
	k := multimetro.Key{
		Metro: l.Metro,
		Name:  l.Name,
		UUID:  l.UUID,
	}
	return []byte(k.Canonical()), nil
}

// ParseLink parses text as a multimetro key. Not a (*Link).UnmarshalText
// method, for the same reason as FormatLink.
func ParseLink[T resource.Resource](text []byte) (Link[T], error) {
	// multimetro.ParseKey takes its input as-is, so surrounding whitespace
	// would end up inside the name and fail every lookup.
	key := multimetro.ParseKey(strings.TrimSpace(string(text)))
	return Link[T]{
		Metro: key.Metro,
		Name:  key.Name,
		UUID:  key.UUID,
	}, nil
}

// TextLink is a Link that also reads and writes as its compact key form, for
// fields holding nothing but a reference (`certificate=my-cert`). Embedding
// Link alone grants no marshaling, so a type needing its own can define it
// without being silently overridden.
type TextLink[T resource.Resource] struct {
	Link[T]
}

func (l TextLink[T]) MarshalText() ([]byte, error) {
	return FormatLink(l.Link)
}

func (l *TextLink[T]) UnmarshalText(text []byte) error {
	link, err := ParseLink[T](text)
	if err != nil {
		return err
	}
	l.Link = link
	return nil
}

// MarshalJSON outputs the struct form, so a link carrying both a name and a
// uuid keeps both. This takes precedence over MarshalText for JSON/YAML.
func (l TextLink[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.Link)
}

// UnmarshalJSON parses both the struct form and the short text form.
// This takes precedence over UnmarshalText for JSON/YAML deserialization.
func (l *TextLink[T]) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return l.UnmarshalText([]byte(text))
	}
	return json.Unmarshal(data, &l.Link)
}

// LinkName models a simple name-only link.
type LinkName[T resource.Resource] string

func (l LinkName[T]) Link() (string, resource.Key, bool) {
	if l == "" {
		return "", nil, false
	}
	var zero T
	return zero.Type().Name, multimetro.Key{
		Name: string(l),
	}, true
}

// MarshalText implements encoding.TextMarshaler.
func (l LinkName[T]) MarshalText() ([]byte, error) {
	return []byte(l), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (l *LinkName[T]) UnmarshalText(text []byte) error {
	*l = LinkName[T](text)
	return nil
}
