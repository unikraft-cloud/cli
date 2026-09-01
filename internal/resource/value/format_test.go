// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package value

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormat(t *testing.T) {
	type s struct {
		Name  string `name:"name"`
		Value string `name:"value"`
	}

	tests := []struct {
		name  string
		input any
		want  string
	}{
		{
			name:  "string",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "int",
			input: 42,
			want:  "42",
		},
		{
			name:  "bool",
			input: true,
			want:  "true",
		},
		{
			name:  "nil",
			input: nil,
			want:  "",
		},
		{
			name:  "slice",
			input: []string{"foo", "bar", "baz"},
			want:  `["foo", "bar", "baz"]`,
		},
		{
			name:  "slice single element",
			input: []string{"foo"},
			want:  `["foo"]`,
		},
		{
			name:  "slice with spaces in elements",
			input: []string{"foo bar", "baz"},
			want:  `["foo bar", "baz"]`,
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  "",
		},
		{
			name:  "map",
			input: map[string]string{"a": "1", "b": "2"},
			want:  "a=1, b=2",
		},
		{
			name:  "empty map",
			input: map[string]string{},
			want:  "",
		},
		{
			name:  "struct",
			input: s{Name: "hello", Value: "world"},
			want:  "name=hello, value=world",
		},
		{
			name:  "struct partial",
			input: s{Name: "hello"},
			want:  "name=hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Render(tt.input, RenderOpts{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFormatNestedPaths pairs with TestParseNestedPaths: a rendered struct is
// fed straight back to Parse (a shortcut flag becomes a --set string), so a
// nested struct has to come out under dotted keys. Rendered inline, its commas
// would reparse as fields of the outer struct.
func TestFormatNestedPaths(t *testing.T) {
	t.Run("nested struct flattens to dotted keys", func(t *testing.T) {
		v := testStructNested{Name: "outer"}
		v.Inner.Label = "x"
		v.Ptr = &testStructInner{Flag: new(true), Label: "y"}

		got, err := Render(&v, RenderOpts{})
		require.NoError(t, err)
		assert.Equal(t, "name=outer, inner.label=x, ptr.flag=true, ptr.label=y", got)
	})

	t.Run("round-trips through Parse", func(t *testing.T) {
		want := testStructNested{Name: "outer"}
		want.Ptr = &testStructInner{Flag: new(false), Label: "y"}
		want.Text.Raw = "whole value"

		rendered, err := Render(&want, RenderOpts{})
		require.NoError(t, err)

		got, err := Parse[testStructNested]([]string{rendered})
		require.NoError(t, err, "rendered as %q", rendered)
		assert.Equal(t, want, got, "rendered as %q", rendered)
	})

	// The mirror of the parse-side check: only the marshalling interfaces
	// decide this direction.
	t.Run("opacity follows MarshalText alone", func(t *testing.T) {
		v := testStructNested{}
		v.WriteTo.Raw = "opaque"
		v.ReadTo.Raw = "flattened"

		got, err := Render(&v, RenderOpts{})
		require.NoError(t, err)
		assert.Equal(t, "read-to.raw=flattened, write-to=opaque", got)
	})

	t.Run("embedded fields render whole, under their type name", func(t *testing.T) {
		v := testStructNested{}
		v.Label = "x"

		got, err := Render(&v, RenderOpts{})
		require.NoError(t, err)
		assert.Equal(t, "test-struct-embedded=label=x", got)

		back, err := Parse[testStructNested]([]string{got})
		require.NoError(t, err)
		assert.Equal(t, v, back)
	})

	t.Run("a text form stays whole", func(t *testing.T) {
		v := testStructNested{}
		v.Text.Raw = "opaque"

		got, err := Render(&v, RenderOpts{})
		require.NoError(t, err)
		assert.Equal(t, "text=opaque", got)
	})

	t.Run("nil pointer renders nothing", func(t *testing.T) {
		got, err := Render(&testStructNested{Name: "outer"}, RenderOpts{})
		require.NoError(t, err)
		assert.Equal(t, "name=outer", got)
	})
}
