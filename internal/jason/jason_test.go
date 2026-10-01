// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package jason

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unmarshalItems[T any](n *Jason[T], items []string) error {
	return Unmarshal([]byte(strings.Join(items, "\n")), n)
}

type Config struct {
	Name  string   `json:"name"`
	Stars int      `json:"stars"`
	Apps  []string `json:"apps"`
}

type Platform struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type Deep struct {
	User struct {
		Name string   `json:"name"`
		Age  int      `json:"age"`
		Role string   `json:"role"`
		Tags []string `json:"tags"`
	} `json:"user"`
}

func TestFromJSON(t *testing.T) {
	var n Jason[Config]
	err := Unmarshal([]byte(`{"name":"HTTPie","stars":54000}`), &n)
	require.NoError(t, err)
	assert.Equal(t, "HTTPie", n.Value.Name)
	assert.Equal(t, 54000, n.Value.Stars)
}

func TestFromNestedJSON_ObjectRoot(t *testing.T) {
	var n Jason[Config]
	err := unmarshalItems(&n, []string{
		"name=HTTPie",
		"stars:=54000",
		"apps[]=Terminal",
		"apps[]=Desktop",
		"apps[]=Web",
	})
	require.NoError(t, err)
	assert.Equal(t, "HTTPie", n.Value.Name)
	assert.Equal(t, 54000, n.Value.Stars)
	assert.Len(t, n.Value.Apps, 3)
	assert.Equal(t, "Terminal", n.Value.Apps[0])
	assert.Equal(t, "Desktop", n.Value.Apps[1])
	assert.Equal(t, "Web", n.Value.Apps[2])
}

func TestFromNestedJSON_TopLevelArray(t *testing.T) {
	var n Jason[[]Platform]
	err := unmarshalItems(&n, []string{
		"[0][type]=platform",
		"[0][name]=terminal",
		"[1][type]=platform",
		"[1][name]=desktop",
	})
	require.NoError(t, err)
	assert.Len(t, n.Value, 2)
	assert.Equal(t, "platform", n.Value[0].Type)
	assert.Equal(t, "terminal", n.Value[0].Name)
	assert.Equal(t, "platform", n.Value[1].Type)
	assert.Equal(t, "desktop", n.Value[1].Name)
}

func TestFromNestedJSON_DeepNesting(t *testing.T) {
	var n Jason[Deep]
	err := unmarshalItems(&n, []string{
		"user[name]=Alice",
		"user[age]:=30",
		"user[role]=admin",
		"user[tags][]=dev",
		"user[tags][]=ops",
	})
	require.NoError(t, err)
	assert.Equal(t, "Alice", n.Value.User.Name)
	assert.Equal(t, 30, n.Value.User.Age)
	assert.Equal(t, "admin", n.Value.User.Role)
	assert.Len(t, n.Value.User.Tags, 2)
	assert.Equal(t, "dev", n.Value.User.Tags[0])
	assert.Equal(t, "ops", n.Value.User.Tags[1])
}

func TestFromNestedJSON_RawValues(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"name=HTTPie",
		"count:=42",
		"active:=true",
		"data:=null",
	})
	require.NoError(t, err)
	assert.Equal(t, "HTTPie", n.Value["name"])

	count, ok := n.Value["count"].(float64)
	assert.True(t, ok)
	assert.InDelta(t, float64(42), count, 0.01)

	active, ok := n.Value["active"].(bool)
	assert.True(t, ok)
	assert.True(t, active)

	assert.Nil(t, n.Value["data"])
}

func TestFromNestedJSON_TopLevelArrayAppend(t *testing.T) {
	var n Jason[[]string]
	err := unmarshalItems(&n, []string{"[]=a", "[]=b", "[]=c"})
	require.NoError(t, err)
	assert.Len(t, n.Value, 3)
	assert.Equal(t, "a", n.Value[0])
	assert.Equal(t, "b", n.Value[1])
	assert.Equal(t, "c", n.Value[2])
}

func TestFromNestedJSON_NestedArrays(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"matrix[0][]=1",
		"matrix[0][]=2",
		"matrix[1][]=3",
		"matrix[1][]=4",
	})
	require.NoError(t, err)

	matrix, ok := n.Value["matrix"].([]any)
	assert.True(t, ok, "matrix should be []any")
	assert.Len(t, matrix, 2)

	row0, ok := matrix[0].([]any)
	assert.True(t, ok, "row0 should be []any")
	assert.Len(t, row0, 2)
	assert.Equal(t, "1", row0[0])
	assert.Equal(t, "2", row0[1])

	row1, ok := matrix[1].([]any)
	assert.True(t, ok, "row1 should be []any")
	assert.Len(t, row1, 2)
	assert.Equal(t, "3", row1[0])
	assert.Equal(t, "4", row1[1])
}

func TestJSONRoundTrip(t *testing.T) {
	var n Jason[Config]
	err := Unmarshal([]byte(`{"name":"test","stars":100}`), &n)
	require.NoError(t, err)

	data, err := json.Marshal(n.Value)
	require.NoError(t, err)

	var got Config
	err = json.Unmarshal(data, &got)
	require.NoError(t, err)

	assert.Equal(t, "test", got.Name)
	assert.Equal(t, 100, got.Stars)
}

// --- Invalid input tests ---

func TestFromNestedJSON_EmptyItems(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, n.Value)
}

func TestFromNestedJSON_NoEqualSign(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"foobar"})
	require.EqualError(t, err, "invalid item: foobar")
}

func TestFromNestedJSON_InvalidRawJSON(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"foo:=invalid"})
	require.Error(t, err)
}

func TestFromNestedJSON_UnclosedBracket(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"foo[baz][quux=FAIL"})
	require.EqualError(t, err, `invalid path in "foo[baz][quux=FAIL": unclosed bracket`)
}

func TestFromNestedJSON_TypeSafetyArrayObjectConflict(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"array[]:=1", "array[key]:=3"})
	require.EqualError(t, err, "type error: cannot perform key-based access on array")
}

func TestFromNestedJSON_TypeSafetyObjectArrayConflict(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"obj[key]=value", "obj[0]=index"})
	require.EqualError(t, err, "type error: cannot perform index-based access on object")
}

func TestFromNestedJSON_MixedShallowAndNested(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"category=tools",
		"search[type]=id",
		"search[id]:=1",
	})
	require.NoError(t, err)
	assert.Equal(t, "tools", n.Value["category"])

	search, ok := n.Value["search"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "id", search["type"])

	id, ok := search["id"].(float64)
	require.True(t, ok)
	assert.InDelta(t, float64(1), id, 0.01)
}

func TestFromNestedJSON_ExplicitArrayIndexOrdering(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"search[type]=keyword",
		"search[keywords][1]=APIs",
		"search[keywords][0]=CLI",
	})
	require.NoError(t, err)

	search, ok := n.Value["search"].(map[string]any)
	require.True(t, ok)

	keywords, ok := search["keywords"].([]any)
	require.True(t, ok)
	assert.Len(t, keywords, 2)
	assert.Equal(t, "CLI", keywords[0])
	assert.Equal(t, "APIs", keywords[1])
}

func TestFromNestedJSON_SparseArrayNullPadding(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"search[type]=platforms",
		"search[platforms][]=Terminal",
		"search[platforms][1]=Desktop",
		"search[platforms][3]=Mobile",
	})
	require.NoError(t, err)

	search, ok := n.Value["search"].(map[string]any)
	require.True(t, ok)

	platforms, ok := search["platforms"].([]any)
	require.True(t, ok)
	assert.Len(t, platforms, 4)
	assert.Equal(t, "Terminal", platforms[0])
	assert.Equal(t, "Desktop", platforms[1])
	assert.Nil(t, platforms[2])
	assert.Equal(t, "Mobile", platforms[3])
}

func TestFromNestedJSON_RawJSONEmbedInNested(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"search[type]=platforms",
		`search[platforms]:=["Terminal","Desktop"]`,
		"search[platforms][]=Web",
		"search[platforms][]=Mobile",
	})
	require.NoError(t, err)

	search, ok := n.Value["search"].(map[string]any)
	require.True(t, ok)

	platforms, ok := search["platforms"].([]any)
	require.True(t, ok)
	assert.Len(t, platforms, 4)
	assert.Equal(t, "Terminal", platforms[0])
	assert.Equal(t, "Desktop", platforms[1])
	assert.Equal(t, "Web", platforms[2])
	assert.Equal(t, "Mobile", platforms[3])
}

func TestFromNestedJSON_TopLevelArrayRawValues(t *testing.T) {
	var n Jason[[]any]
	err := unmarshalItems(&n, []string{
		"[]:=1",
		"[]:=2",
		"[]:=3",
	})
	require.NoError(t, err)
	assert.Len(t, n.Value, 3)

	v0, ok := n.Value[0].(float64)
	require.True(t, ok)
	assert.InDelta(t, float64(1), v0, 0.01)

	v1, ok := n.Value[1].(float64)
	require.True(t, ok)
	assert.InDelta(t, float64(2), v1, 0.01)

	v2, ok := n.Value[2].(float64)
	require.True(t, ok)
	assert.InDelta(t, float64(3), v2, 0.01)
}

func TestFromNestedJSON_DeepNestingAllFeatures(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"shallow=value",
		"object[key]=value",
		"array[]:=1",
		"array[1]:=2",
		"array[2]:=3",
		"very[nested][json][3][httpie][power][]=Amaze",
	})
	require.NoError(t, err)
	assert.Equal(t, "value", n.Value["shallow"])

	obj, ok := n.Value["object"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", obj["key"])

	arr, ok := n.Value["array"].([]any)
	require.True(t, ok)
	assert.Len(t, arr, 3)
	assert.InDelta(t, float64(1), arr[0], 0.01)
	assert.InDelta(t, float64(2), arr[1], 0.01)
	assert.InDelta(t, float64(3), arr[2], 0.01)

	very, ok := n.Value["very"].(map[string]any)
	require.True(t, ok)
	nested, ok := very["nested"].(map[string]any)
	require.True(t, ok)

	jsonArr, ok := nested["json"].([]any)
	require.True(t, ok, "json should be an array since [3] is an array index")
	require.Greater(t, len(jsonArr), 3)

	httpieObj, ok := jsonArr[3].(map[string]any)
	require.True(t, ok, "jsonArr[3] should be a map")
	power, ok := httpieObj["httpie"].(map[string]any)
	require.True(t, ok)
	powerArr, ok := power["power"].([]any)
	require.True(t, ok)
	assert.Equal(t, "Amaze", powerArr[0])
}

func TestFromNestedJSON_OversizedArrayIndex(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"items[44444440]=x"})
	require.EqualError(t, err, "array index 44444440 exceeds maximum allowed index 10000")
}

func TestFromNestedJSON_NegativeArrayIndex(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"items[-1]=x"})
	require.EqualError(t, err, "array index -1 exceeds maximum allowed index 10000")
}

func TestFromNestedJSON_ValueOverride(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"user[name]:=411",
		"user[name]=string",
	})
	require.NoError(t, err)

	user, ok := n.Value["user"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", user["name"])
}

func TestFromNestedJSONThenJSON_OverridesField(t *testing.T) {
	var n Jason[Config]
	err := unmarshalItems(&n, []string{
		"name=HTTPie",
		"stars:=54000",
	})
	require.NoError(t, err)
	assert.Equal(t, "HTTPie", n.Value.Name)
	assert.Equal(t, 54000, n.Value.Stars)

	err = Unmarshal([]byte(`{"name":"overridden"}`), &n)
	require.NoError(t, err)
	assert.Equal(t, "overridden", n.Value.Name)
	assert.Equal(t, 54000, n.Value.Stars, "stars should be unchanged")
}

func TestFromJSONThenFromNestedJSON_OverridesField(t *testing.T) {
	var n Jason[Config]
	err := Unmarshal([]byte(`{"name":"original","stars":100}`), &n)
	require.NoError(t, err)
	assert.Equal(t, "original", n.Value.Name)
	assert.Equal(t, 100, n.Value.Stars)

	err = unmarshalItems(&n, []string{"name=overridden"})
	require.NoError(t, err)
	assert.Equal(t, "overridden", n.Value.Name)
	assert.Equal(t, 100, n.Value.Stars, "stars should be unchanged")
}

func TestFromNestedJSONThenJSON_LastValueWins(t *testing.T) {
	var n Jason[Config]
	err := unmarshalItems(&n, []string{
		"name=first",
		"stars:=54000",
	})
	require.NoError(t, err)
	assert.Equal(t, "first", n.Value.Name)

	err = Unmarshal([]byte(`{"name":"second"}`), &n)
	require.NoError(t, err)
	assert.Equal(t, "second", n.Value.Name)

	err = unmarshalItems(&n, []string{"name=third"})
	require.NoError(t, err)
	assert.Equal(t, "third", n.Value.Name)
	assert.Equal(t, 54000, n.Value.Stars)
}

func TestFromNestedJSON_EscapedBracketsInKeyName(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`foo\[bar\]:=1`,
	})
	require.NoError(t, err)
	assert.InDelta(t, float64(1), n.Value["foo[bar]"], 0.01)
}

func TestFromNestedJSON_EscapedBracketsAsKeys(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`baz[\[]:=2`,
		`baz[\]]:=3`,
	})
	require.NoError(t, err)

	baz, ok := n.Value["baz"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(2), baz["["], 0.01)
	assert.InDelta(t, float64(3), baz["]"], 0.01)
}

func TestFromNestedJSON_EscapedBackslash(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`backslash[\\]:=1`,
	})
	require.NoError(t, err)

	bs, ok := n.Value["backslash"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(1), bs["\\"], 0.01)
}

func TestFromNestedJSON_AppendWithNestedKey(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`arr[][key]=value`,
	})
	require.NoError(t, err)

	arr, ok := n.Value["arr"].([]any)
	require.True(t, ok, "arr should be []any")
	require.Len(t, arr, 1)

	obj, ok := arr[0].(map[string]any)
	require.True(t, ok, "arr[0] should be map[string]any")
	assert.Equal(t, "value", obj["key"])
}

func TestFromNestedJSON_AppendWithNestedKeyAndRawValue(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`arr[][count]:=42`,
	})
	require.NoError(t, err)

	arr, ok := n.Value["arr"].([]any)
	require.True(t, ok, "arr should be []any")
	require.Len(t, arr, 1)

	obj, ok := arr[0].(map[string]any)
	require.True(t, ok, "arr[0] should be map[string]any")

	count, ok := obj["count"].(float64)
	require.True(t, ok)
	assert.InDelta(t, float64(42), count, 0.01)
}

func TestFromNestedJSON_AppendWithDeepNestedKey(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`data[][inner][deep]=yes`,
	})
	require.NoError(t, err)

	arr, ok := n.Value["data"].([]any)
	require.True(t, ok, "data should be []any")
	require.Len(t, arr, 1)

	inner, ok := arr[0].(map[string]any)
	require.True(t, ok, "data[0] should be map[string]any")

	deep, ok := inner["inner"].(map[string]any)
	require.True(t, ok, "data[0].inner should be map[string]any")
	assert.Equal(t, "yes", deep["deep"])
}

func TestFromNestedJSON_EscapedIntegerAsStringKey(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`object[\1]=stringified`,
		`object[\100]=same`,
		`object[\124514]=big-index`,
		`object[\1234a666]=mixed-key`,
		`array[1]=indexified`,
	})
	require.NoError(t, err)

	obj, ok := n.Value["object"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "stringified", obj["1"])
	assert.Equal(t, "same", obj["100"])
	assert.Equal(t, "big-index", obj["124514"])
	assert.Equal(t, "mixed-key", obj["\\1234a666"])

	arr, ok := n.Value["array"].([]any)
	require.True(t, ok)
	assert.Len(t, arr, 2)
	assert.Nil(t, arr[0])
	assert.Equal(t, "indexified", arr[1])
}

// --- JSON literal merge tests ---

func TestFromNestedJSON_RootJSONObjectLiteral(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`{"key":"value"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "value", n.Value["key"])
}

func TestFromNestedJSON_RootJSONArrayLiteral(t *testing.T) {
	var n Jason[[]any]
	err := unmarshalItems(&n, []string{
		`[1,2,3]`,
	})
	require.NoError(t, err)
	assert.Len(t, n.Value, 3)
	assert.InDelta(t, float64(1), n.Value[0], 0.01)
	assert.InDelta(t, float64(2), n.Value[1], 0.01)
	assert.InDelta(t, float64(3), n.Value[2], 0.01)
}

func TestFromNestedJSON_RootJSONObjectMerge(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`{"a":1}`,
		`{"b":2}`,
	})
	require.NoError(t, err)
	assert.InDelta(t, float64(1), n.Value["a"], 0.01)
	assert.InDelta(t, float64(2), n.Value["b"], 0.01)
}

func TestFromNestedJSON_RootJSONObjectMergeWithPathItems(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`{"predefined":"value"}`,
		"extra=item",
	})
	require.NoError(t, err)
	assert.Equal(t, "value", n.Value["predefined"])
	assert.Equal(t, "item", n.Value["extra"])
}

func TestFromNestedJSON_RootJSONArrayWithAppends(t *testing.T) {
	var n Jason[[]any]
	err := unmarshalItems(&n, []string{
		`[1,2]`,
		"[]:=3",
		"[]:=4",
	})
	require.NoError(t, err)
	assert.Len(t, n.Value, 4)
	assert.InDelta(t, float64(1), n.Value[0], 0.01)
	assert.InDelta(t, float64(2), n.Value[1], 0.01)
	assert.InDelta(t, float64(3), n.Value[2], 0.01)
	assert.InDelta(t, float64(4), n.Value[3], 0.01)
}

func TestFromNestedJSON_RootJSONObjectOverride(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"name=original",
		`{"name":"overridden"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "overridden", n.Value["name"])
}

func TestFromNestedJSON_RootJSONEmptyObject(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`{}`,
		"key=val",
	})
	require.NoError(t, err)
	assert.Equal(t, "val", n.Value["key"])
}

func TestFromNestedJSON_JSONLiteralWithWhitespace(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`  {"key":  "value"}  `,
	})
	require.NoError(t, err)
	assert.Equal(t, "value", n.Value["key"])
}

func TestFromNestedJSON_WhitespaceSeparatedItems(t *testing.T) {
	var n Jason[Config]
	err := Unmarshal([]byte("name=HTTPie stars:=54000\r\napps[]=Terminal\tapps[]=Desktop"), &n)
	require.NoError(t, err)
	assert.Equal(t, "HTTPie", n.Value.Name)
	assert.Equal(t, 54000, n.Value.Stars)
	assert.Equal(t, []string{"Terminal", "Desktop"}, n.Value.Apps)
}

func TestFromNestedJSON_SingleItemWithSpaceValue(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte("name=HTTPie CLI"), &n)
	require.NoError(t, err)
	assert.Equal(t, "HTTPie CLI", n.Value["name"])
}

func TestFromNestedJSON_SpacedValueFollowedByAnotherItem(t *testing.T) {
	var n Jason[Config]
	err := Unmarshal([]byte("name=HTTPie CLI stars:=54000"), &n)
	require.NoError(t, err)
	assert.Equal(t, "HTTPie CLI", n.Value.Name)
	assert.Equal(t, 54000, n.Value.Stars)
}

func TestFromNestedJSON_BracePrefixedInvalidJSON_FallsThroughToPathParsing(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`{foo=bar`,
	})
	require.NoError(t, err)
	assert.Equal(t, "bar", n.Value["{foo"])
}

func TestFromNestedJSON_JSONObjectLatexWithKeys(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`{"nested":{"a":1}}`,
	})
	require.NoError(t, err)
	nested, ok := n.Value["nested"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(1), nested["a"], 0.01)
}

func TestFromNestedJSON_JSONLiteralInMiddle(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"first=initial",
		`{"second":"middle-json"}`,
		"third=final",
	})
	require.NoError(t, err)
	assert.Equal(t, "initial", n.Value["first"])
	assert.Equal(t, "middle-json", n.Value["second"])
	assert.Equal(t, "final", n.Value["third"])
}

func TestFromNestedJSON_JSONLiteralAtEnd(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"first=initial",
		"second=another",
		`{"third":"json-at-end"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "initial", n.Value["first"])
	assert.Equal(t, "another", n.Value["second"])
	assert.Equal(t, "json-at-end", n.Value["third"])
}

func TestFromNestedJSON_JSONLiteralAtEndWithOverride(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"name=first",
		"count:=42",
		`{"name":"override"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "override", n.Value["name"])
	count, ok := n.Value["count"].(float64)
	require.True(t, ok)
	assert.InDelta(t, float64(42), count, 0.01)
}

func TestFromNestedJSON_MultipleJSONLiteralsMixedWithPaths(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`{"a":1}`,
		"middle=path",
		`{"b":2}`,
		"last=item",
		`{"c":3}`,
	})
	require.NoError(t, err)
	assert.InDelta(t, float64(1), n.Value["a"], 0.01)
	assert.Equal(t, "path", n.Value["middle"])
	assert.InDelta(t, float64(2), n.Value["b"], 0.01)
	assert.Equal(t, "item", n.Value["last"])
	assert.InDelta(t, float64(3), n.Value["c"], 0.01)
}

func TestFromNestedJSON_NestedJSONLiteralThenPathMerge(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`{"data":{"inner":"value"}}`,
		"data[another]=field",
		"extra=append",
	})
	require.NoError(t, err)
	assert.Equal(t, "append", n.Value["extra"])

	data, ok := n.Value["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", data["inner"])
	assert.Equal(t, "field", data["another"])
}

func TestFromNestedJSON_PathThenNestedJSONLiteral(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"first=init",
		`{"nested":{"deep":"value"}}`,
		"nested[also]=set",
	})
	require.NoError(t, err)
	assert.Equal(t, "init", n.Value["first"])

	nested, ok := n.Value["nested"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", nested["deep"])
	assert.Equal(t, "set", nested["also"])
}

func TestFromNestedJSON_JSONArrayReplacesRoot(t *testing.T) {
	var n Jason[[]any]
	err := unmarshalItems(&n, []string{
		"[]:=1",
		"[]:=2",
		`[3,4]`,
	})
	require.NoError(t, err)
	assert.Len(t, n.Value, 2)
	assert.InDelta(t, float64(3), n.Value[0], 0.01)
	assert.InDelta(t, float64(4), n.Value[1], 0.01)
}

func TestFromNestedJSON_TopLevelArrayFromAny(t *testing.T) {
	var n Jason[any]
	err := unmarshalItems(&n, []string{
		"[0][type]=platform",
		"[0][name]=desktop",
		"[1][type]=platform",
		"[1][name]=web",
	})
	require.NoError(t, err)

	arr, ok := n.Value.([]any)
	require.True(t, ok, "n.Value should be []any")
	require.Len(t, arr, 2)

	item0, ok := arr[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "platform", item0["type"])
	assert.Equal(t, "desktop", item0["name"])

	item1, ok := arr[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "platform", item1["type"])
	assert.Equal(t, "web", item1["name"])
}

func TestFromNestedJSON_ArrayRootRejectsObjectLiteralMerge(t *testing.T) {
	var n Jason[any]
	err := unmarshalItems(&n, []string{
		"[]:=1",
		"[]:=2",
		`{"a":1}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "array-rooted")
}

func TestFromNestedJSON_ArrayIndexOverflow(t *testing.T) {
	var n Jason[any]
	err := unmarshalItems(&n, []string{"[99999999999999999999]=x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed index")
	assert.NotContains(t, err.Error(), "key-based access")
}

func TestFromNestedJSON_JSONArrayInMiddleReplacedByPath(t *testing.T) {
	var n Jason[[]any]
	err := unmarshalItems(&n, []string{
		`[1,2]`,
		`[3,4]`,
		"[]:=5",
	})
	require.NoError(t, err)
	assert.Len(t, n.Value, 3)
	assert.InDelta(t, float64(3), n.Value[0], 0.01)
	assert.InDelta(t, float64(4), n.Value[1], 0.01)
	assert.InDelta(t, float64(5), n.Value[2], 0.01)
}

// --- Regression: array-root vs. shallow key/raw conflicts ---
//
// buildNestedJSON used to strip every item's leading path segment whenever
// ANY item in the batch triggered array-root mode, instead of only stripping
// items that themselves started with a bracket. A plain "key=value" mixed
// into an array-rooted body would fall through the "empty path" branch and
// silently replace the whole root with its own scalar (or, depending on
// item order, produce a misleading "unexpected container type" error).

func TestFromNestedJSON_ArrayRootRejectsShallowKeyAssignment(t *testing.T) {
	var n Jason[any]
	err := unmarshalItems(&n, []string{
		"[]=x",
		"name=y",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot perform key-based access on array")
}

func TestFromNestedJSON_ShallowKeyThenArrayRootRejectedSameError(t *testing.T) {
	var n Jason[any]
	err := unmarshalItems(&n, []string{
		"name=y",
		"[]=x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot perform key-based access on array",
		"order should not change the error family: both orderings must fail the same way")
}

func TestFromNestedJSON_ArrayRootRejectsShallowRawScalar(t *testing.T) {
	var n Jason[any]
	err := unmarshalItems(&n, []string{
		"[0][type]=platform",
		"count:=42",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot perform key-based access on array")
}

func TestFromNestedJSON_ArrayRootOnlyBracketItemsStillWorks(t *testing.T) {
	var n Jason[any]
	err := unmarshalItems(&n, []string{
		"[0][type]=platform",
		"[1][type]=desktop",
	})
	require.NoError(t, err)

	arr, ok := n.Value.([]any)
	require.True(t, ok)
	require.Len(t, arr, 2)

	item0, ok := arr[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "platform", item0["type"])
}

func TestDebugTree_ArrayRootRejectsShallowKeyAssignment(t *testing.T) {
	_, err := debugTree([]string{
		"[0][type]=platform",
		"name=value",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot perform key-based access on array")
}

func TestDebugTree_ArrayRootOnlyBracketItems(t *testing.T) {
	output, err := debugTree([]string{
		"[0][type]=platform",
	})
	require.NoError(t, err)
	assert.Contains(t, output, "Root is array: true")
}

// --- Regression: same-line raw JSON values with unusual (but valid) whitespace ---
//
// The same-line item splitter used to accept any prefix of a raw (":=")
// value as a candidate split point, then asked "does the JSON decoder see a
// complete value right after this space?" A bare scalar token like `1`
// followed by a space satisfies that check even when it's still inside an
// unclosed outer object, so `data:={"a": 1 , "b": 2} next=x` (space before
// the comma) split apart in the middle of the JSON and failed with a
// truncated, misleading error. The fix requires a raw candidate's value to
// be complete, valid JSON before it can be accepted as a boundary at all.

func TestFromNestedJSON_SameLineRawJSONSpaceBeforeComma(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte(`data:={"a": 1 , "b": 2} next=x`), &n)
	require.NoError(t, err)

	data, ok := n.Value["data"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(1), data["a"], 0.01)
	assert.InDelta(t, float64(2), data["b"], 0.01)
	assert.Equal(t, "x", n.Value["next"])
}

func TestFromNestedJSON_SameLineRawJSONSpaceBeforeClosingBrace(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte(`data:={"a": 1 } next=x`), &n)
	require.NoError(t, err)

	data, ok := n.Value["data"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(1), data["a"], 0.01)
	assert.Equal(t, "x", n.Value["next"])
}

func TestFromNestedJSON_SameLineRawJSONCompleteNestedObjectInsideStillOpenOuter(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte(`data:={"nested": {"a": 1} , "b": 2} next=x`), &n)
	require.NoError(t, err)

	data, ok := n.Value["data"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(2), data["b"], 0.01)

	nested, ok := data["nested"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(1), nested["a"], 0.01)

	assert.Equal(t, "x", n.Value["next"])
}

func TestFromNestedJSON_SameLineRawJSONArrayWithSpacedNumbers(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte(`data:=[1, 2, 3] next=x`), &n)
	require.NoError(t, err)

	data, ok := n.Value["data"].([]any)
	require.True(t, ok)
	assert.Len(t, data, 3)
	assert.Equal(t, "x", n.Value["next"])
}

func TestFromNestedJSON_SameLineRawJSONQuotedStringWithSpaces(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte(`msg:="hello world" next=x`), &n)
	require.NoError(t, err)
	assert.Equal(t, "hello world", n.Value["msg"])
	assert.Equal(t, "x", n.Value["next"])
}

func TestFromNestedJSON_SameLineTwoRawJSONItemsUnambiguous(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte(`a:={"x":1} b:={"y":2}`), &n)
	require.NoError(t, err)

	a, ok := n.Value["a"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(1), a["x"], 0.01)

	b, ok := n.Value["b"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, float64(2), b["y"], 0.01)
}

func TestFromNestedJSON_GenuinelyMalformedSameLineRawJSONStillErrors(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte(`data:={"a": 1 , "b": 2 next=x`), &n)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid raw JSON value`)
}

// --- Escaping edge cases ---

func TestFromNestedJSON_EscapedEqualsSignInKeyName(t *testing.T) {
	// Documented in APICmd.Help() under ESCAPING as `key\=value=test`.
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`key\=value=test`,
	})
	require.NoError(t, err)
	assert.Equal(t, "test", n.Value["key=value"])
}

func TestFromNestedJSON_EscapedColonAloneLeavesRealEqualsAsOperator(t *testing.T) {
	// Escaping only the ':' of a `:=` pair does not escape the '=' that
	// follows: `key\:=value` scans as key "key:" plus a literal ('=')
	// assignment, not a raw-value assignment.
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`key\:=value`,
	})
	require.NoError(t, err)
	assert.Equal(t, "value", n.Value["key:"])
}

func TestFromNestedJSON_EscapedColonAndEqualsEmbedsLiteralRawOperator(t *testing.T) {
	// To get a literal ":=" inside a key, both characters must be escaped.
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`key\:\=extra=val`,
	})
	require.NoError(t, err)
	assert.Equal(t, "val", n.Value["key:=extra"])
}

func TestFromNestedJSON_ColonNotFollowedByEqualsIsLiteral(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"time:val=x",
	})
	require.NoError(t, err)
	assert.Equal(t, "x", n.Value["time:val"])
}

func TestFromNestedJSON_EscapeStripsBackslashForArbitraryCharacter(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`key\z=value`,
	})
	require.NoError(t, err)
	assert.Equal(t, "value", n.Value["keyz"])
}

func TestFromNestedJSON_MultipleBackslashesCollapsePairwise(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`key[\\\\]=val`,
	})
	require.NoError(t, err)

	key, ok := n.Value["key"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "val", key[`\\`])
}

func TestFromNestedJSON_EscapedLeadingBracketDoesNotTriggerArrayRoot(t *testing.T) {
	var n Jason[any]
	err := unmarshalItems(&n, []string{
		`\[0\]=value`,
	})
	require.NoError(t, err)

	m, ok := n.Value.(map[string]any)
	require.True(t, ok, "root must stay an object, not become a top-level array")
	assert.Equal(t, "value", m["[0]"])
}

func TestFromNestedJSON_IntermediateEscapedDigitSegmentForcesMapNotArray(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`data[\1][name]=value`,
	})
	require.NoError(t, err)

	data, ok := n.Value["data"].(map[string]any)
	require.True(t, ok, "escaping the digit segment must force an object, even though it is not the leaf")

	inner, ok := data["1"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", inner["name"])
}

func TestFromNestedJSON_UnescapedDigitIntermediateSegmentForcesArray(t *testing.T) {
	// Contrast with TestFromNestedJSON_IntermediateEscapedDigitSegmentForcesMapNotArray:
	// without the backslash, the same shape produces an array.
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		`data[1][name]=value`,
	})
	require.NoError(t, err)

	data, ok := n.Value["data"].([]any)
	require.True(t, ok)
	require.Len(t, data, 2)
	assert.Nil(t, data[0])

	inner, ok := data[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", inner["name"])
}

// --- Whitespace-splitting edge cases ---

func TestFromNestedJSON_WhitespaceInsideBracketsBecomesLiteralStringKey(t *testing.T) {
	// A space inside brackets is preserved literally rather than trimmed,
	// so it fails the numeric-index check and produces a map with a
	// space-containing key instead of an array.
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"foo[ 5]=x",
	})
	require.NoError(t, err)

	foo, ok := n.Value["foo"].(map[string]any)
	require.True(t, ok, "whitespace in the index must force map semantics, not array semantics")
	assert.Equal(t, "x", foo[" 5"])
}

func TestFromNestedJSON_MultiWordValueWithNoOtherItems(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte("msg=one two three four"), &n)
	require.NoError(t, err)
	assert.Equal(t, "one two three four", n.Value["msg"])
}

func TestFromNestedJSON_ValueContainingLiteralEqualsThenSpaceThenNextItem(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal([]byte("note=x= y next=z"), &n)
	require.NoError(t, err)
	assert.Equal(t, "x= y", n.Value["note"])
	assert.Equal(t, "z", n.Value["next"])
}

// --- Array index boundary edge cases ---

func TestFromNestedJSON_LeadingZeroIndexIsDecimalNotOctal(t *testing.T) {
	// arr[010] must land at index 10, not octal 8 - regression guard in
	// case the index parser is ever changed to strconv.ParseInt(seg, 0, 64).
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"arr[010]=x",
	})
	require.NoError(t, err)

	arr, ok := n.Value["arr"].([]any)
	require.True(t, ok)
	require.Len(t, arr, 11)
	assert.Equal(t, "x", arr[10])
}

func TestFromNestedJSON_ArrayIndexBoundary_MaxAllowedSucceeds(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"arr[10000]=x",
	})
	require.NoError(t, err)

	arr, ok := n.Value["arr"].([]any)
	require.True(t, ok)
	require.Len(t, arr, 10001)
	assert.Equal(t, "x", arr[10000])
}

func TestFromNestedJSON_ArrayIndexBoundary_OneOverMaxFails(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"arr[10001]=x",
	})
	require.EqualError(t, err, "array index 10001 exceeds maximum allowed index 10000")
}

func TestFromNestedJSON_NegativeArrayIndexOverflowClassifiedAsRangeError(t *testing.T) {
	// Mirrors TestFromNestedJSON_ArrayIndexOverflow but negative: a
	// top-level bracket commits the root to array semantics unconditionally
	// (it's syntactic, not content-dependent), so an unparseable index still
	// reaches assignAtSlice's strconv.ParseInt and is correctly classified
	// as a range error rather than a generic syntax error.
	var n Jason[any]
	err := unmarshalItems(&n, []string{"[-99999999999999999999]=x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed index")
	assert.NotContains(t, err.Error(), "key-based access")
}

func TestFromNestedJSON_OverflowingIndexNestedUnderNamedKeyBecomesStringKey(t *testing.T) {
	// Contrast with the top-level case above: nested under a named key,
	// array-vs-map is decided by isNumericKey (strconv.Atoi) *before* any
	// index is actually assigned. Atoi fails the same way ParseInt would,
	// but isNumericKey collapses every failure to "not numeric" - so rather
	// than surfacing the overflow, it silently falls back to map semantics
	// with the oversized number as a literal string key.
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"items[99999999999999999999]=x"})
	require.NoError(t, err)

	items, ok := n.Value["items"].(map[string]any)
	require.True(t, ok, "an index too large to parse must fall back to a map key, not error")
	assert.Equal(t, "x", items["99999999999999999999"])
}

func TestFromNestedJSON_PlusSignedArrayIndex(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"arr[+5]=x",
	})
	require.NoError(t, err)

	arr, ok := n.Value["arr"].([]any)
	require.True(t, ok)
	require.Len(t, arr, 6)
	assert.Equal(t, "x", arr[5])
}

func TestFromNestedJSON_NegativeZeroArrayIndexIsIndexZero(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"arr[-0]=x",
	})
	require.NoError(t, err)

	arr, ok := n.Value["arr"].([]any)
	require.True(t, ok)
	require.Len(t, arr, 1)
	assert.Equal(t, "x", arr[0])
}

// --- Scalar/container escalation edge cases ---
//
// Once a path holds a scalar, assigning deeper under that same path
// discards the scalar and builds the requested container instead - the
// mirror image of the already-covered "last value wins" overwrite tests,
// just at differing depths rather than the same depth.

func TestFromNestedJSON_ScalarOverwrittenByNestedObjectAssignment(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"foo=bar",
		"foo[baz]=qux",
	})
	require.NoError(t, err)

	foo, ok := n.Value["foo"].(map[string]any)
	require.True(t, ok, "the earlier scalar must be discarded, not preserved alongside the object")
	assert.Equal(t, "qux", foo["baz"])
}

func TestFromNestedJSON_ScalarOverwrittenByArrayAppendAssignment(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{
		"foo=bar",
		"foo[]=x",
	})
	require.NoError(t, err)

	foo, ok := n.Value["foo"].([]any)
	require.True(t, ok, "the earlier scalar must be discarded, not preserved alongside the array")
	require.Len(t, foo, 1)
	assert.Equal(t, "x", foo[0])
}

// --- Boundary robustness edge cases: clean errors, never panics ---

func TestFromNestedJSON_EmptyRawValueErrorsCleanly(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"foo:="})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid raw JSON value ""`)
}

func TestFromNestedJSON_EmptyLiteralValueSucceeds(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"foo="})
	require.NoError(t, err)

	foo, ok := n.Value["foo"].(string)
	require.True(t, ok, "foo must be present as a string, not absent or nil")
	assert.Empty(t, foo)
}

func TestFromNestedJSON_MinimalUnclosedBracketAtEOF(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"foo["})
	require.EqualError(t, err, `invalid path in "foo[": unclosed bracket`)
}

func TestFromNestedJSON_LoneOpenBracket(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"["})
	require.EqualError(t, err, `invalid path in "[": unclosed bracket`)
}

func TestFromNestedJSON_LoneCloseBracket(t *testing.T) {
	var n Jason[map[string]any]
	err := unmarshalItems(&n, []string{"]"})
	require.EqualError(t, err, "invalid item: ]")
}

// --- Unmarshal: empty/whitespace input against non-object target types ---
//
// The empty-input fallback in Unmarshal attempts to decode the literal "{}",
// which only satisfies object-shaped (or `any`) targets - that attempt's
// error is discarded rather than propagated, so a slice or scalar T is just
// left at its zero value instead of surfacing a spurious error.

func TestUnmarshal_EmptyInputIntoSliceTargetLeavesZeroValue(t *testing.T) {
	var n Jason[[]string]
	err := Unmarshal([]byte(""), &n)
	require.NoError(t, err)
	assert.Nil(t, n.Value)
}

func TestUnmarshal_WhitespaceOnlyInputIntoSliceTargetLeavesZeroValue(t *testing.T) {
	var n Jason[[]string]
	err := Unmarshal([]byte("   \t\n  "), &n)
	require.NoError(t, err)
	assert.Nil(t, n.Value)
}

func TestUnmarshal_EmptyInputIntoScalarTargetLeavesZeroValue(t *testing.T) {
	var n Jason[int]
	err := Unmarshal([]byte(""), &n)
	require.NoError(t, err)
	assert.Zero(t, n.Value)
}

func TestUnmarshal_EmptyInputNeverClobbersAlreadyPopulatedSlice(t *testing.T) {
	n := Jason[[]string]{Value: []string{"existing"}}
	err := Unmarshal([]byte(""), &n)
	require.NoError(t, err)
	assert.Equal(t, []string{"existing"}, n.Value)
}

func TestUnmarshal_EmptyInputIntoAnyTargetSucceeds(t *testing.T) {
	var n Jason[any]
	err := Unmarshal([]byte(""), &n)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, n.Value)
}

func TestUnmarshal_NilInputIntoMapTargetSucceeds(t *testing.T) {
	var n Jason[map[string]any]
	err := Unmarshal(nil, &n)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, n.Value)
}

func Example_debugTree() {
	items := []string{
		"name=HTTPie",
		"stars:=54000",
		"apps[]=Terminal",
		"apps[]=Desktop",
		"apps[]=Web",
	}
	output, err := debugTree(items)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Print(output)
	// Output:
	// Input items:
	//   0: [name]=HTTPie  → path=[name]  isRaw=false  value="HTTPie"
	//   1: [stars]:=54000  → path=[stars]  isRaw=true  value="54000"
	//   2: [apps][]=Terminal  → path=[apps ]  isRaw=false  value="Terminal"
	//   3: [apps][]=Desktop  → path=[apps ]  isRaw=false  value="Desktop"
	//   4: [apps][]=Web  → path=[apps ]  isRaw=false  value="Web"
	//
	// Root is array: false
	//
	// Assignments (in order):
	//   path=[name]  val="HTTPie"
	//   path=[stars]  val=54000
	//   path=[apps → ]  val="Terminal"
	//   path=[apps → ]  val="Desktop"
	//   path=[apps → ]  val="Web"
	//
	// Final tree:
	// {
	//   apps = [
	//     [0] = "Terminal"
	//     [1] = "Desktop"
	//     [2] = "Web"
	//   ]
	//   name = "HTTPie"
	//   stars = 54000
	// }
	//
	// JSON:
	// {
	//   "apps": [
	//     "Terminal",
	//     "Desktop",
	//     "Web"
	//   ],
	//   "name": "HTTPie",
	//   "stars": 54000
	// }
}
