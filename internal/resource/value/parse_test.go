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

type testStruct struct {
	Name  string `name:"name" json:"name"`
	Value int    `name:"value" json:"value"`
}

// testStructCollections confirms comma splitting applies only to a struct's
// own top-level fields - a nested slice/map takes its value verbatim.
type testStructCollections struct {
	Labels []string          `name:"labels"`
	Meta   map[string]string `name:"meta"`
	Env    map[string]string `name:"env"`
}

type testStructNested struct {
	Name    string              `name:"name"`
	Inner   testStructInner     `name:"inner"`
	Ptr     *testStructInner    `name:"ptr"`
	Hidden  testStructInner     `name:"-"`
	Text    testStructWithText  `name:"text"`
	ReadTo  testStructReadOnly  `name:"read-to"`
	WriteTo testStructWriteOnly `name:"write-to"`
	TestStructEmbedded
}

// TestStructEmbedded is exported so that embedding it produces an exported
// field, which is the only way the anonymous case is reached at all.
type TestStructEmbedded struct {
	Label string `name:"label"`
}

// testStructReadOnly converts only from a string, so parsing takes its whole
// value while rendering still flattens it.
type testStructReadOnly struct {
	Raw string `name:"raw"`
}

func (t *testStructReadOnly) UnmarshalText(text []byte) error {
	t.Raw = string(text)
	return nil
}

// testStructWriteOnly converts only to a string, the mirror of
// testStructReadOnly.
type testStructWriteOnly struct {
	Raw string `name:"raw"`
}

func (t testStructWriteOnly) MarshalText() ([]byte, error) {
	return []byte(t.Raw), nil
}

type testStructInner struct {
	Flag  *bool  `name:"flag"`
	Label string `name:"label"`
}

// testStructWithText owns its whole value, so a dotted key must not reach
// past it into its fields.
type testStructWithText struct {
	Raw string `name:"raw"`
}

func (t *testStructWithText) UnmarshalText(text []byte) error {
	t.Raw = string(text)
	return nil
}

func (t testStructWithText) MarshalText() ([]byte, error) {
	return []byte(t.Raw), nil
}

// TestParseNestedPaths covers dotted keys, which are how the flat key=value
// form reaches a nested struct field. Without them a nested field is only
// settable by giving the outer field a text form of its own, which then has
// to encode every subfield in one value.
func TestParseNestedPaths(t *testing.T) {
	t.Run("nested field", func(t *testing.T) {
		got, err := Parse[testStructNested]([]string{"name=outer,inner.label=x"})
		require.NoError(t, err)
		assert.Equal(t, "outer", got.Name)
		assert.Equal(t, "x", got.Inner.Label)
	})

	t.Run("allocates a nil pointer", func(t *testing.T) {
		got, err := Parse[testStructNested]([]string{"ptr.flag=true"})
		require.NoError(t, err)
		require.NotNil(t, got.Ptr)
		require.NotNil(t, got.Ptr.Flag)
		assert.True(t, *got.Ptr.Flag)
	})

	t.Run("several keys reach the same nested struct", func(t *testing.T) {
		got, err := Parse[testStructNested]([]string{"ptr.label=y,ptr.flag=false"})
		require.NoError(t, err)
		require.NotNil(t, got.Ptr)
		assert.Equal(t, "y", got.Ptr.Label)
		require.NotNil(t, got.Ptr.Flag)
		assert.False(t, *got.Ptr.Flag)
	})

	t.Run("order does not matter", func(t *testing.T) {
		a, err := Parse[testStructNested]([]string{"inner.label=x,name=outer"})
		require.NoError(t, err)
		b, err := Parse[testStructNested]([]string{"name=outer,inner.label=x"})
		require.NoError(t, err)
		assert.Equal(t, a, b)
	})

	t.Run("unknown nested key reports the whole path", func(t *testing.T) {
		_, err := Parse[testStructNested]([]string{"inner.bogus=1"})
		require.ErrorContains(t, err, "unknown fields: [inner.bogus]")
	})

	t.Run("unknown head reports the whole path", func(t *testing.T) {
		_, err := Parse[testStructNested]([]string{"nope.label=1"})
		require.ErrorContains(t, err, "unknown fields: [nope.label]")
	})

	t.Run("name:\"-\" is not reachable", func(t *testing.T) {
		_, err := Parse[testStructNested]([]string{"hidden.label=x"})
		require.ErrorContains(t, err, "unknown fields: [hidden.label]")
	})

	t.Run("a bare word is not a field name", func(t *testing.T) {
		_, err := Parse[testStructNested]([]string{"inner=x"})
		require.ErrorContains(t, err, `invalid value "x": expected <key>=<value>`)
	})

	t.Run("does not descend past a text form", func(t *testing.T) {
		_, err := Parse[testStructNested]([]string{"text.raw=x"})
		require.ErrorContains(t, err, "unknown fields: [text.raw]")

		got, err := Parse[testStructNested]([]string{"text=whole value"})
		require.NoError(t, err)
		assert.Equal(t, "whole value", got.Text.Raw)
	})

	// Only UnmarshalText decides this direction. Sharing one check with
	// Render would let a marshal-only type block parsing, and vice versa.
	t.Run("opacity follows UnmarshalText alone", func(t *testing.T) {
		_, err := Parse[testStructNested]([]string{"read-to.raw=x"})
		require.ErrorContains(t, err, "unknown fields: [read-to.raw]")

		got, err := Parse[testStructNested]([]string{"write-to.raw=x"})
		require.NoError(t, err)
		assert.Equal(t, "x", got.WriteTo.Raw)
	})

	// An embedded field is addressed as a whole under its type name, so a
	// dotted key must not also reach into it.
	t.Run("embedded fields are not descended into", func(t *testing.T) {
		_, err := Parse[testStructNested]([]string{"test-struct-embedded.label=x"})
		require.ErrorContains(t, err, "unknown fields: [test-struct-embedded.label]")

		got, err := Parse[testStructNested]([]string{"test-struct-embedded=label=x"})
		require.NoError(t, err)
		assert.Equal(t, "x", got.Label)
	})
}

func TestParseStandalone(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		got, err := Parse[string]([]string{"hello"})
		require.NoError(t, err)
		assert.Equal(t, "hello", got)
	})

	t.Run("int", func(t *testing.T) {
		got, err := Parse[int]([]string{"42"})
		require.NoError(t, err)
		assert.Equal(t, 42, got)
	})

	t.Run("bool", func(t *testing.T) {
		got, err := Parse[bool]([]string{"true"})
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("float", func(t *testing.T) {
		got, err := Parse[float64]([]string{"3.14"})
		require.NoError(t, err)
		assert.InDelta(t, 3.14, got, 1e-10)
	})

	t.Run("empty slice input", func(t *testing.T) {
		got, err := Parse[[]string]([]string{})
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("nil input", func(t *testing.T) {
		got, err := Parse[string](nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestParseJSON(t *testing.T) {
	t.Run("slice", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`["foo", "bar"]`})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo", "bar"}, got)
	})

	t.Run("slice with commas in values", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`["foo,bar", "baz"]`})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo,bar", "baz"}, got)
	})

	t.Run("json array combined with a literal element", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`["foo", "bar"]`, "baz,qux"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo", "bar", "baz,qux"}, got)
	})

	t.Run("int slice", func(t *testing.T) {
		got, err := Parse[[]int]([]string{`[1, 2, 3]`})
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2, 3}, got)
	})

	t.Run("empty array", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"[]"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("map", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{`{"a": "1", "b": "2"}`})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, got)
	})

	t.Run("map with commas in values", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{`{"key": "a,b,c"}`})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": "a,b,c"}, got)
	})

	t.Run("map combined with csv", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{`{"a": "1"}`, "b=2"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, got)
	})

	t.Run("empty object", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"{}"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("struct", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{`{"name": "hello", "value": 42}`})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Name: "hello", Value: 42}, got)
	})

	t.Run("slice with escaped quotes", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`["say \"hello\"", "world"]`})
		require.NoError(t, err)
		assert.Equal(t, []string{`say "hello"`, "world"}, got)
	})

	t.Run("slice with backslashes", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`["C:\\Users\\test", "foo\\bar"]`})
		require.NoError(t, err)
		assert.Equal(t, []string{`C:\Users\test`, `foo\bar`}, got)
	})

	t.Run("slice with newlines and tabs", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`["line1\nline2", "col1\tcol2"]`})
		require.NoError(t, err)
		assert.Equal(t, []string{"line1\nline2", "col1\tcol2"}, got)
	})

	t.Run("slice with unicode escapes", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`["\u0048ello", "\u0057orld"]`})
		require.NoError(t, err)
		assert.Equal(t, []string{"Hello", "World"}, got)
	})

	t.Run("slice with mixed escaped characters", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`["he said \"hi\\there\"\nok"]`})
		require.NoError(t, err)
		assert.Equal(t, []string{"he said \"hi\\there\"\nok"}, got)
	})

	t.Run("map with escaped quotes in keys and values", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{`{"k\"ey": "val\"ue"}`})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{`k"ey`: `val"ue`}, got)
	})

	t.Run("map with backslashes in values", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{`{"path": "C:\\Users\\test"}`})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"path": `C:\Users\test`}, got)
	})

	t.Run("map with newlines in values", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{`{"msg": "line1\nline2"}`})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"msg": "line1\nline2"}, got)
	})

	t.Run("struct with escaped quotes", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{`{"name": "say \"hi\"", "value": 1}`})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Name: `say "hi"`, Value: 1}, got)
	})
}

func TestParseCSV(t *testing.T) {
	t.Run("slice single", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"foo"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo"}, got)
	})

	t.Run("slice comma is not a separator", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"foo,bar"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo,bar"}, got)
	})

	t.Run("slice multiple inputs, one element each", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"foo,bar", "baz,qux"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo,bar", "baz,qux"}, got)
	})

	t.Run("slice spaces preserved verbatim", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"foo, bar, baz"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo, bar, baz"}, got)
	})

	t.Run("slice empty input is a single empty element, not skipped", func(t *testing.T) {
		got, err := Parse[[]string]([]string{""})
		require.NoError(t, err)
		assert.Equal(t, []string{""}, got)
	})

	t.Run("map single pair", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"key=value"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": "value"}, got)
	})

	t.Run("map multiple inputs", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"a=1", "b=2"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, got)
	})

	t.Run("struct", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{"name=hello,value=42"})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Name: "hello", Value: 42}, got)
	})

	t.Run("struct trailing comma skips empty item", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{"value=42,"})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Value: 42}, got)
	})

	t.Run("struct repeated comma skips empty item", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{"name=hello,,value=42"})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Name: "hello", Value: 42}, got)
	})

	t.Run("struct unknown field after comma", func(t *testing.T) {
		_, err := Parse[testStruct]([]string{"name=hello,unknown=1"})
		require.ErrorContains(t, err, "unknown fields: [unknown]")
	})

	t.Run("struct slice field takes one verbatim element", func(t *testing.T) {
		got, err := Parse[testStructCollections]([]string{"labels=foo|bar|baz"})
		require.NoError(t, err)
		assert.Equal(t, testStructCollections{Labels: []string{"foo|bar|baz"}}, got)
	})

	t.Run("struct map field takes one verbatim pair", func(t *testing.T) {
		got, err := Parse[testStructCollections]([]string{"meta=a=1|b=2"})
		require.NoError(t, err)
		assert.Equal(t, testStructCollections{Meta: map[string]string{"a": "1|b=2"}}, got)
	})

	t.Run("struct map field with another key behaves the same way", func(t *testing.T) {
		got, err := Parse[testStructCollections]([]string{"env=a=1|b=2"})
		require.NoError(t, err)
		assert.Equal(t, testStructCollections{Env: map[string]string{"a": "1|b=2"}}, got)
	})

	t.Run("slice with quotes in values", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`say "hello"`, `it's fine`})
		require.NoError(t, err)
		assert.Equal(t, []string{`say "hello"`, `it's fine`}, got)
	})

	t.Run("slice with backslashes", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`C:\Users\test`})
		require.NoError(t, err)
		assert.Equal(t, []string{`C:\Users\test`}, got)
	})

	t.Run("slice with equals signs stays one element", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"a=b,c=d"})
		require.NoError(t, err)
		assert.Equal(t, []string{"a=b,c=d"}, got)
	})

	t.Run("slice whitespace-only input preserved verbatim", func(t *testing.T) {
		got, err := Parse[[]string]([]string{" , , "})
		require.NoError(t, err)
		assert.Equal(t, []string{" , , "}, got)
	})

	t.Run("map value with equals signs", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"key=a=b=c"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": "a=b=c"}, got)
	})

	t.Run("map value with quotes", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{`key=say "hello"`})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": `say "hello"`}, got)
	})

	t.Run("map value with backslashes", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{`path=C:\Users\test`})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"path": `C:\Users\test`}, got)
	})

	t.Run("map key with no value", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"key="})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": ""}, got)
	})

	t.Run("map key with no equals", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"key"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": ""}, got)
	})

	t.Run("struct with spaces in value", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{"name=hello world,value=42"})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Name: "hello world", Value: 42}, got)
	})
}

func TestParseSliceLiteralValues(t *testing.T) {
	t.Run("value with comma", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"a,b,c"})
		require.NoError(t, err)
		assert.Equal(t, []string{"a,b,c"}, got)
	})

	t.Run("value with comma and equals", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"key1=value1,key2=value2"})
		require.NoError(t, err)
		assert.Equal(t, []string{"key1=value1,key2=value2"}, got)
	})

	t.Run("multiple repeated inputs, each verbatim", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"a,b", "c,d"})
		require.NoError(t, err)
		assert.Equal(t, []string{"a,b", "c,d"}, got)
	})

	t.Run("whitespace preserved, no trimming", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"  a,b  "})
		require.NoError(t, err)
		assert.Equal(t, []string{"  a,b  "}, got)
	})
}

func TestParseMapLiteralValues(t *testing.T) {
	t.Run("value with comma", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"key=a,b,c"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": "a,b,c"}, got)
	})

	t.Run("value with comma and equals", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"VAR=key1=value1,key2=value2"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"VAR": "key1=value1,key2=value2"}, got)
	})

	t.Run("multiple entries with literal values", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"A=a,b", "B=c,d"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"A": "a,b", "B": "c,d"}, got)
	})

	t.Run("key trimmed, value preserved", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"  key  =  a,b  "})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": "  a,b  "}, got)
	})

	t.Run("entries without a key skipped", func(t *testing.T) {
		for _, input := range []string{"", "   ", "=value"} {
			got, err := Parse[map[string]string]([]string{input})
			require.NoError(t, err)
			assert.Equal(t, map[string]string{}, got, "input %q", input)
		}
	})
}
