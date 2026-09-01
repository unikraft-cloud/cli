// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"fmt"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/patch"
)

type flagPort struct {
	Source uint32 `name:"source"`
	Dest   uint32 `name:"dest"`
}

func (p *flagPort) UnmarshalText(text []byte) error {
	_, err := fmt.Sscanf(string(text), "%d:%d", &p.Source, &p.Dest)
	return err
}

type flagFixture struct {
	Metro string `field:"metro,short" create:"set,required" flag:"metro" help:"Metro." placeholder:"metro"`
	Name  string `field:",short" create:"set" edit:"set" flag:"name" help:"Name." short:"n"`

	Limits struct {
		Soft uint64 `field:",long" create:"set" edit:"set" flag:"soft-limit" help:"Soft limit."`
	}

	Tags  []string    `field:",long" create:"set" edit:"set,add,del" flag:"tag" sep:"none" help:"Tag."`
	Ports []*flagPort `field:",embed" create:"set" flag:"port" sep:"none" help:"Port."`

	// Neither is exposed: Secret is settable but opts out by having no flag
	// tag, Plain is not settable at all.
	Secret string `field:"secret,long" create:"set"`
	Plain  string `field:"plain,long"`
}

// parseGenerated builds flags for the fixture, splices them into a throwaway
// command, and parses args against it, exactly as the real parser does.
func parseGenerated(t *testing.T, create bool, args ...string) (*FlagSet, error) {
	t.Helper()
	fields, err := resource.FieldsFromStruct(flagFixture{})
	require.NoError(t, err)

	group := "flag-edit"
	if create {
		group = "flag-create"
	}
	set, err := GenerateFlags(fields, create, group)
	if err != nil {
		return nil, err
	}

	return set, parseInto(t, set, args...)
}

// parseInto splices a generated flag set into a throwaway command and parses
// args against it, exactly as the real parser does.
func parseInto(t *testing.T, set *FlagSet, args ...string) error {
	t.Helper()
	var root struct {
		Create struct{} `cmd:""`
	}
	parser, err := kong.New(&root, kong.Name("t"), kong.Exit(func(int) {}),
		kong.PostBuild(func(k *kong.Kong) error {
			k.Model.Node.Children[0].Flags = append(k.Model.Node.Children[0].Flags, set.Flags...)
			return nil
		}))
	require.NoError(t, err)
	_, err = parser.Parse(append([]string{"create"}, args...))
	return err
}

func flagNames(set *FlagSet) []string {
	names := make([]string, 0, len(set.Flags))
	for _, f := range set.Flags {
		names = append(names, f.Name)
	}
	return names
}

func TestGenerateFlags(t *testing.T) {
	t.Run("flags are opt-in", func(t *testing.T) {
		set, err := parseGenerated(t, true)
		require.NoError(t, err)
		assert.Equal(t, []string{"metro", "name", "soft-limit", "tag", "port"}, flagNames(set))
	})

	t.Run("edit only exposes editable fields", func(t *testing.T) {
		set, err := parseGenerated(t, false)
		require.NoError(t, err)
		assert.Equal(t, []string{"name", "soft-limit", "tag"}, flagNames(set))
	})

	t.Run("presentation comes from the field tag", func(t *testing.T) {
		set, err := parseGenerated(t, true)
		require.NoError(t, err)
		byName := map[string]string{}
		for _, f := range set.Flags {
			byName[f.Name] = f.Help
		}
		assert.Equal(t, "Metro.", byName["metro"])
		assert.Equal(t, "Soft limit.", byName["soft-limit"])
		for _, f := range set.Flags {
			if f.Name == "name" {
				assert.Equal(t, 'n', f.Short)
			}
			assert.Equal(t, "flag-create", f.Group.Key)
		}
	})

	t.Run("unset flags are not applied", func(t *testing.T) {
		set, err := parseGenerated(t, true, "--metro=fra")
		require.NoError(t, err)
		spec := patch.PatchSpec{Create: true}
		require.NoError(t, set.Apply(&spec))
		assert.Equal(t, []string{"metro"}, keysOf(spec.SetTyped))
	})

	t.Run("values arrive typed, never as strings", func(t *testing.T) {
		set, err := parseGenerated(t, true,
			"--metro=fra", "--soft-limit=5", "--tag=a", "--tag=b,c", "--port=443:8080")
		require.NoError(t, err)

		spec := patch.PatchSpec{Create: true}
		require.NoError(t, set.Apply(&spec))

		assert.Equal(t, "fra", spec.SetTyped["metro"])
		assert.Equal(t, uint64(5), spec.SetTyped["limits.soft"])
		// The comma is part of the tag, not a separator.
		assert.Equal(t, []string{"a", "b,c"}, spec.SetTyped["tags"])
		assert.Equal(t, []*flagPort{{Source: 443, Dest: 8080}}, spec.SetTyped["ports"])
		assert.Empty(t, spec.Set, "generated flags must not go through strings")
	})

	t.Run("typed values match the field's patch template", func(t *testing.T) {
		fields, err := resource.FieldsFromStruct(flagFixture{})
		require.NoError(t, err)

		set, err := parseGenerated(t, true, "--metro=fra", "--port=443:8080", "--tag=a")
		require.NoError(t, err)
		spec := patch.PatchSpec{Create: true}
		require.NoError(t, set.Apply(&spec))

		// PatchedFields rejects a typed value whose type differs from the
		// template, so a clean pass proves every slot matched.
		_, err = patch.PatchedFields(t.Context(), fields, spec)
		require.NoError(t, err)
	})

	// The shortcut flags merged with --set for collection fields, and the
	// integration tests rely on it: -e X=1 --set runtime.env=Y=2 keeps both.
	t.Run("a collection takes both a flag and --set", func(t *testing.T) {
		fields, err := resource.FieldsFromStruct(flagFixture{})
		require.NoError(t, err)

		set, err := parseGenerated(t, true, "--metro=fra", "--tag=from-flag")
		require.NoError(t, err)
		spec := patch.PatchSpec{Create: true, Set: map[string][]string{
			"tags": {"from-set"},
		}}
		require.NoError(t, set.Apply(&spec))

		patched, err := patch.PatchedFields(t.Context(), fields, spec)
		require.NoError(t, err)
		tags, ok := resource.Field{Subfields: patched}.Get("tags")
		require.True(t, ok)
		// --set lands first, the flag's entries after it.
		assert.Equal(t, []string{"from-set", "from-flag"}, tags.Create.Set)
	})

	t.Run("a generated flag overrides --set for the same field", func(t *testing.T) {
		set, err := parseGenerated(t, true, "--metro=fra")
		require.NoError(t, err)
		spec := patch.PatchSpec{Create: true, Set: map[string][]string{
			"metro": {"sfo"},
		}}
		require.NoError(t, set.Apply(&spec))

		fields, err := resource.FieldsFromStruct(flagFixture{})
		require.NoError(t, err)
		patched, err := patch.PatchedFields(t.Context(), fields, spec)
		require.NoError(t, err)
		metro, ok := resource.Field{Subfields: patched}.Get("metro")
		require.True(t, ok)
		assert.Equal(t, "fra", metro.Create.Set)
	})
}

// A path can cover several fields of different types, as when a resource and
// the backend it wraps both have a "type". The flag fits one; the rest are
// dropped rather than failing the command.
func TestTypedValueSkipsFieldsItDoesNotFit(t *testing.T) {
	fields := []resource.Field{
		{Name: "type", Value: "", Create: &resource.Patch{Set: ""}},
		{Name: "type", Value: 0, Create: &resource.Patch{Set: 0}},
	}

	patched, err := patch.PatchedFields(t.Context(), fields, patch.PatchSpec{
		Create:   true,
		SetTyped: map[string]any{"type": "instance"},
	})
	require.NoError(t, err)

	var set []any
	for _, f := range patched {
		if f.Create != nil {
			set = append(set, f.Create.Set)
		}
	}
	assert.Equal(t, []any{"instance"}, set)
}

func TestTypedValueFittingNoFieldIsRejected(t *testing.T) {
	fields := []resource.Field{
		{Name: "type", Value: 0, Create: &resource.Patch{Set: 0}},
	}

	_, err := patch.PatchedFields(t.Context(), fields, patch.PatchSpec{
		Create:   true,
		SetTyped: map[string]any{"type": "instance"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no field of a matching type for: [type]")
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestFlagOnUnsettableFieldIsRejected(t *testing.T) {
	type bad struct {
		A string `field:"a" flag:"a"`
	}
	_, err := resource.FieldsFromStruct(bad{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `has flag:"a" but is not settable`)
}

func TestGenerateFlagsRejectsDuplicateNames(t *testing.T) {
	type dup struct {
		A string `field:"a" create:"set" flag:"same"`
		B string `field:"b" create:"set" flag:"same"`
	}
	fields, err := resource.FieldsFromStruct(dup{})
	require.NoError(t, err)

	_, err = GenerateFlags(fields, true, "flag-create")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate generated flag --same")
}
