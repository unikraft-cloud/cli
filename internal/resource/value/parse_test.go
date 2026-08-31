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

type testStructJSON struct {
	Name   string `name:"name"`
	Config string `name:"config"`
}

func TestSplitTopLevel(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr string
	}{
		{name: "plain", in: "a=1,b=2", want: []string{"a=1", "b=2"}},
		{name: "object", in: `a=1,b={"x":2}`, want: []string{"a=1", `b={"x":2}`}},
		{name: "array", in: "a=1,b=[1,2,3]", want: []string{"a=1", "b=[1,2,3]"}},
		{name: "nested", in: `a={"x":{"y":[1,2]}},b=2`, want: []string{`a={"x":{"y":[1,2]}}`, "b=2"}},
		{name: "comma inside string", in: `a={"x":"y,z"},b=2`, want: []string{`a={"x":"y,z"}`, "b=2"}},
		{name: "brace inside string", in: `a={"x":"}"},b=2`, want: []string{`a={"x":"}"}`, "b=2"}},
		{name: "escaped quote inside string", in: `a={"x":"y\",z"},b=2`, want: []string{`a={"x":"y\",z"}`, "b=2"}},
		{name: "trailing comma drops empty tail", in: "a=1,", want: []string{"a=1"}},
		{name: "unmatched closer still splits", in: "a=1,b=x],c=3", want: []string{"a=1", "b=x]", "c=3"}},
		{name: "invalid utf-8 passes through", in: "a=\xff\xfe,b=2", want: []string{"a=\xff\xfe", "b=2"}},
		{name: "quote mid-value stays literal", in: `image=my"img,at=/rom0,name=r`, want: []string{`image=my"img`, "at=/rom0", "name=r"}},
		{name: "brace mid-value stays literal", in: "name=a{b,rom=c}d", want: []string{"name=a{b", "rom=c}d"}},
		{name: "bracket mid-value stays literal", in: "image=a[b,at=/x]c", want: []string{"image=a[b", "at=/x]c"}},
		{name: "spaces are not trimmed around the separator", in: "a= 1 ,b=2", want: []string{"a= 1 ", "b=2"}},
		{name: "empty", in: "", want: nil},

		{name: "unmatched opener", in: "a=1,b={,c=3", wantErr: `missing "}"`},
		{name: "unterminated quote at value start", in: `config="oops,name=x`, wantErr: "unterminated quote"},
		{name: "mismatched pair", in: `a={"x":1],b=2`, wantErr: `missing "}"`},
		{name: "mismatched pair the other way", in: "a=[1},b=2", wantErr: `missing "]"`},
		{name: "wrong closer", in: `a={"x":[1}},b=2`, wantErr: `missing "]"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitTopLevel(tt.in)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseStructJSONValue(t *testing.T) {
	t.Run("object config", func(t *testing.T) {
		got, err := Parse[testStructJSON]([]string{`name=logger,config={"level":"debug"}`})
		require.NoError(t, err)
		assert.Equal(t, testStructJSON{Name: "logger", Config: `{"level":"debug"}`}, got)
	})

	t.Run("config keeps its own commas", func(t *testing.T) {
		got, err := Parse[testStructJSON]([]string{`name=logger,config={"a":1,"b":[2,3]}`})
		require.NoError(t, err)
		assert.Equal(t, testStructJSON{Name: "logger", Config: `{"a":1,"b":[2,3]}`}, got)
	})

	t.Run("config keeps commas inside strings", func(t *testing.T) {
		got, err := Parse[testStructJSON]([]string{`config={"msg":"a,b"},name=logger`})
		require.NoError(t, err)
		assert.Equal(t, testStructJSON{Name: "logger", Config: `{"msg":"a,b"}`}, got)
	})

	t.Run("array config", func(t *testing.T) {
		got, err := Parse[testStructJSON]([]string{`name=logger,config=[1,2]`})
		require.NoError(t, err)
		assert.Equal(t, testStructJSON{Name: "logger", Config: "[1,2]"}, got)
	})

	t.Run("scalar config", func(t *testing.T) {
		got, err := Parse[testStructJSON]([]string{`name=logger,config=5`})
		require.NoError(t, err)
		assert.Equal(t, testStructJSON{Name: "logger", Config: "5"}, got)
	})

	t.Run("a stray closer does not glue later fields together", func(t *testing.T) {
		got, err := Parse[testStructJSON]([]string{"config=x],name=logger"})
		require.NoError(t, err)
		assert.Equal(t, testStructJSON{Name: "logger", Config: "x]"}, got)
	})

	t.Run("a truncated config is reported, not split mid-value", func(t *testing.T) {
		_, err := Parse[testStructJSON]([]string{`name=logger,config={"level":"debug"`})
		require.ErrorContains(t, err, `missing "}"`)
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
