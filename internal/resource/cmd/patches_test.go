// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/jason"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/patch"
)

func TestSetValuesDecode(t *testing.T) {
	type CLI struct {
		SetArgs
		Targets []string `arg:"" optional:""`
	}

	item := func(s string) jason.Item {
		i, err := jason.ParseItem(s)
		require.NoError(t, err)
		return i
	}

	tests := []struct {
		name        string
		args        []string
		wantSet     SetValues
		wantTargets []string
	}{
		{
			name:    "pair",
			args:    []string{"--set", "name=a=b,c"},
			wantSet: SetValues{{Key: "name", Value: "a=b,c"}},
		},
		{
			name:        "pair does not take more pairs",
			args:        []string{"--set", "name=a", "tags=b"},
			wantSet:     SetValues{{Key: "name", Value: "a"}},
			wantTargets: []string{"tags=b"},
		},
		{
			name:        "name then items",
			args:        []string{"--set", "scale", "policy=on", "stateful=true", "target"},
			wantSet:     SetValues{{Key: "scale", Items: []jason.Item{item("policy=on"), item("stateful=true")}}},
			wantTargets: []string{"target"},
		},
		{
			name:    "equals form of name then items",
			args:    []string{"--set=scale", `{"policy":"on"}`},
			wantSet: SetValues{{Key: "scale", Items: []jason.Item{item(`{"policy":"on"}`)}}},
		},
		{
			name: "repeated",
			args: []string{"--set", "scale", "policy=on", "--set", "name=a"},
			wantSet: SetValues{
				{Key: "scale", Items: []jason.Item{item("policy=on")}},
				{Key: "name", Value: "a"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cli CLI
			parser, err := kong.New(&cli, kong.Vars{"name": "resource"})
			require.NoError(t, err)

			_, err = parser.Parse(tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSet, cli.Set)
			assert.Equal(t, tt.wantTargets, cli.Targets)
		})
	}

	t.Run("name without items", func(t *testing.T) {
		var cli CLI
		parser, err := kong.New(&cli, kong.Vars{"name": "resource"})
		require.NoError(t, err)

		_, err = parser.Parse([]string{"--set", "scale", "target"})
		require.ErrorContains(t, err, `expected "<name>=<value>" or "scale <item>..." but got "scale"`)
	})
}

type setScaleFixture struct {
	Policy   string `json:"policy" field:",long"`
	Stateful bool   `json:"stateful" field:",long"`
}

type setFixture struct {
	Name  string          `field:",short" create:"set"`
	Scale setScaleFixture `field:",embed" create:"set"`
	Tags  []string        `field:",long" create:"set"`
}

func patchedSet(t *testing.T, set SetValues) ([]resource.Field, error) {
	t.Helper()
	fields, err := resource.FieldsFromStruct(setFixture{})
	require.NoError(t, err)

	spec := patch.PatchSpec{Create: true}
	require.NoError(t, SetArgs{Set: set}.Apply(&spec))
	return patch.PatchedFields(t.Context(), fields, spec)
}

func TestSetItems(t *testing.T) {
	var cli struct{ SetArgs }
	parser, err := kong.New(&cli, kong.Vars{"name": "resource"})
	require.NoError(t, err)

	_, err = parser.Parse([]string{
		"--set", "scale", "policy=on", "stateful=true",
		"--set", "tags", "[]=a", "[]=b",
		"--set", "name=x",
	})
	require.NoError(t, err)

	patched, err := patchedSet(t, cli.Set)
	require.NoError(t, err)

	root := resource.Field{Subfields: patched}
	scale, ok := root.Get("scale")
	require.True(t, ok)
	assert.Equal(t, setScaleFixture{Policy: "on", Stateful: true}, scale.Create.Set)
	tags, ok := root.Get("tags")
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, tags.Create.Set)
	name, ok := root.Get("name")
	require.True(t, ok)
	assert.Equal(t, "x", name.Create.Set)
}

func TestSetItemsErrors(t *testing.T) {
	item := func(s string) []jason.Item {
		i, err := jason.ParseItem(s)
		require.NoError(t, err)
		return []jason.Item{i}
	}

	t.Run("mixed with a pair", func(t *testing.T) {
		_, err := patchedSet(t, SetValues{
			{Key: "tags", Value: "a"},
			{Key: "tags", Items: item("[]=b")},
		})
		require.EqualError(t, err, "failed to unpack set value for tags: cannot mix <name>=<value> with items")
	})

	t.Run("wrong type", func(t *testing.T) {
		_, err := patchedSet(t, SetValues{{Key: "scale", Items: item("stateful=maybe")}})
		require.ErrorContains(t, err, "failed to unpack set value for scale")
	})

	t.Run("unknown field", func(t *testing.T) {
		_, err := patchedSet(t, SetValues{{Key: "nope", Items: item("a=1")}})
		require.ErrorContains(t, err, "unknown fields: [nope]")
	})
}
