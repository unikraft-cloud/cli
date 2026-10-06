// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package jason

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type textValue string

func (v *textValue) UnmarshalText(text []byte) error {
	*v = textValue(strings.ToUpper(string(text)))
	return nil
}

type customJSON struct {
	Enabled bool `json:"enabled"`
}

func (c *customJSON) UnmarshalJSON(data []byte) error {
	type alias customJSON
	return json.Unmarshal(data, (*alias)(c))
}

type Embedded struct {
	Inner int `json:"inner"`
}

type Typed struct {
	Embedded
	Bool     bool    `json:"bool"`
	Int      int     `json:"int"`
	Uint     uint8   `json:"uint"`
	Float    float64 `json:"float"`
	String   string  `json:"string"`
	Ptr      *bool   `json:"ptr"`
	Quoted   int     `json:"quoted,string"`
	Untagged int
	Ints     []int             `json:"ints"`
	Map      map[string]bool   `json:"map"`
	Any      any               `json:"any"`
	Text     textValue         `json:"text"`
	Custom   customJSON        `json:"custom"`
	Nested   struct{ N int }   `json:"nested"`
	Nesteds  []struct{ N int } `json:"nesteds"`
	Skipped  int               `json:"-"`
	Dash     int               `json:"-,"`
}

func parseAll(t *testing.T, items ...string) []Item {
	t.Helper()
	parsed := make([]Item, 0, len(items))
	for _, item := range items {
		p, err := ParseItem(item)
		require.NoError(t, err)
		parsed = append(parsed, p)
	}
	return parsed
}

func TestUnmarshalItems_LiteralsDecodeIntoTargetType(t *testing.T) {
	var v Typed
	err := UnmarshalItems(parseAll(t,
		"bool=true",
		"int=-42",
		"uint=7",
		"float=1.5e3",
		"string=true",
		"ptr=false",
		"quoted=12",
		"untagged=3",
		"inner=9",
		"ints[]=1",
		"ints[]=2",
		"map[a]=1",
		"any=5",
		"text=abc",
		"custom[enabled]=true",
		"nested[N]=4",
		"nesteds[0][N]=5",
		"-=6",
	), &v)
	require.NoError(t, err)

	assert.True(t, v.Bool)
	assert.Equal(t, -42, v.Int)
	assert.Equal(t, uint8(7), v.Uint)
	assert.InDelta(t, 1500.0, v.Float, 0)
	assert.Equal(t, "true", v.String)
	assert.Equal(t, new(false), v.Ptr)
	assert.Equal(t, 12, v.Quoted)
	assert.Equal(t, 3, v.Untagged)
	assert.Equal(t, 9, v.Inner)
	assert.Equal(t, []int{1, 2}, v.Ints)
	assert.Equal(t, map[string]bool{"a": true}, v.Map)
	assert.Equal(t, "5", v.Any)
	assert.Equal(t, textValue("ABC"), v.Text)
	assert.True(t, v.Custom.Enabled)
	assert.Equal(t, 4, v.Nested.N)
	assert.Equal(t, []struct{ N int }{{N: 5}}, v.Nesteds)
	assert.Equal(t, 6, v.Dash)
}

func TestUnmarshalItems_RawStringIsNotCoerced(t *testing.T) {
	var v Typed
	err := UnmarshalItems(parseAll(t, `bool:="true"`), &v)
	require.ErrorContains(t, err, "cannot unmarshal string")
}

func TestUnmarshalItems_UnparseableLiteralFailsAsString(t *testing.T) {
	for _, item := range []string{"bool=yes", "int=0x10", "int=", "float=NaN"} {
		t.Run(item, func(t *testing.T) {
			var v Typed
			err := UnmarshalItems(parseAll(t, item), &v)
			require.ErrorContains(t, err, "cannot unmarshal string")
		})
	}
}

func TestUnmarshalItems_AnyTargetKeepsStrings(t *testing.T) {
	var v any
	err := UnmarshalItems(parseAll(t, "a=1", "b=true", "c[]=2"), &v)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": "1", "b": "true", "c": []any{"2"}}, v)
}

func TestUnmarshalItems_CaseInsensitiveFieldName(t *testing.T) {
	var v Typed
	err := UnmarshalItems(parseAll(t, "BOOL=true", "UnTagged=2"), &v)
	require.NoError(t, err)
	assert.True(t, v.Bool)
	assert.Equal(t, 2, v.Untagged)
}

func TestUnmarshalItems_UnknownFieldStaysString(t *testing.T) {
	var v map[string]any
	err := UnmarshalItems(parseAll(t, "x=1"), &v)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"x": "1"}, v)
}

func TestUnmarshalItems_JSONLiteralMergesWithItems(t *testing.T) {
	var v Typed
	err := UnmarshalItems(parseAll(t, `{"int":1,"string":"x"}`, "int=2"), &v)
	require.NoError(t, err)
	assert.Equal(t, 2, v.Int)
	assert.Equal(t, "x", v.String)
}

func TestUnmarshal_LiteralDecodesIntoTargetType(t *testing.T) {
	var n Jason[Config]
	err := Unmarshal([]byte("name=HTTPie stars=54000"), &n)
	require.NoError(t, err)
	assert.Equal(t, 54000, n.Value.Stars)
}
