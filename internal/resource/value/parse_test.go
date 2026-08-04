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
	Name   string `name:"name" json:"name"`
	Value  int    `name:"value" json:"value"`
	Config string `name:"config" json:"config,omitempty"`
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

	t.Run("slice combined with csv", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`["foo", "bar"]`, "baz,qux"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo", "bar", "baz", "qux"}, got)
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

	t.Run("slice comma-separated", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"foo,bar"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo", "bar"}, got)
	})

	t.Run("slice multiple inputs", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"foo,bar", "baz,qux"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo", "bar", "baz", "qux"}, got)
	})

	t.Run("slice spaces trimmed", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"foo, bar, baz"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo", "bar", "baz"}, got)
	})

	t.Run("slice empty items skipped", func(t *testing.T) {
		got, err := Parse[[]string]([]string{",foo,,bar,"})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo", "bar"}, got)
	})

	t.Run("map single pair", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"key=value"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": "value"}, got)
	})

	t.Run("map multiple pairs", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"a=1,b=2"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, got)
	})

	t.Run("map multiple inputs", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"a=1", "b=2"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, got)
	})

	t.Run("map spaces trimmed", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{"a=1, b=2"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, got)
	})

	t.Run("map empty items skipped", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{",a=1,,b=2,"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a": "1", "b": "2"}, got)
	})

	t.Run("struct", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{"name=hello,value=42"})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Name: "hello", Value: 42}, got)
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

	t.Run("slice with equals signs", func(t *testing.T) {
		got, err := Parse[[]string]([]string{"a=b,c=d"})
		require.NoError(t, err)
		assert.Equal(t, []string{"a=b", "c=d"}, got)
	})

	t.Run("slice with only whitespace items", func(t *testing.T) {
		got, err := Parse[[]string]([]string{" , , "})
		require.NoError(t, err)
		assert.Empty(t, got)
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

	t.Run("struct with json value containing commas", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{`name=test,config={"a":1,"b":"x,y"}`})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Name: "test", Config: `{"a":1,"b":"x,y"}`}, got)
	})

	t.Run("struct with nested json value", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{`name=test,config={"db":{"host":"localhost","port":5432}}`})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Name: "test", Config: `{"db":{"host":"localhost","port":5432}}`}, got)
	})

	t.Run("struct with json array value", func(t *testing.T) {
		got, err := Parse[testStruct]([]string{`name=test,config=[1,2,3]`})
		require.NoError(t, err)
		assert.Equal(t, testStruct{Name: "test", Config: `[1,2,3]`}, got)
	})

	t.Run("map with json value containing commas", func(t *testing.T) {
		got, err := Parse[map[string]string]([]string{`key={"a":1,"b":"x,y"}`})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"key": `{"a":1,"b":"x,y"}`}, got)
	})

	t.Run("slice with json-like non-json content preserves braces", func(t *testing.T) {
		got, err := Parse[[]string]([]string{`foo,{bar,baz}`})
		require.NoError(t, err)
		assert.Equal(t, []string{"foo", "{bar,baz}"}, got)
	})
}
