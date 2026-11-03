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
