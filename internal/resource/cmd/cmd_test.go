// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	resourcet "unikraft.com/cli/internal/resource/testing"
	"unikraft.com/cli/internal/types"
	xkong "unikraft.com/cli/internal/x/kong"
	"unikraft.com/cloud/sdk/platform/group"
)

// setupTestEnv creates a TestEnv with standard test data.
func setupTestEnv() *resourcet.TestEnv {
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{
		ID:        "id-test1",
		Name:      "test1",
		State:     "pending",
		URL:       "https://example.com",
		Hidden:    "hidden-test1",
		Invisible: "invisible-test1",
		Created:   time.Now().Add(-3 * 24 * time.Hour),
		Settings: resourcet.TestSettings{
			Foo:   42,
			Bar:   "hello",
			Score: 10.0,
			Flag:  true,
		},
		Authors: []resourcet.TestAuthor{
			{Name: "Alice", Email: "alice@example.com"},
			{Name: "Bob", Email: "bob@example.com"},
		},
		Tags:  []string{"prod", "web"},
		Usage: types.MeterUsage[int]{Used: 70, Total: 100},
	})
	env.Add(resourcet.TestResource{
		ID:        "id-test2",
		Name:      "test2",
		State:     "pending",
		URL:       "https://example.org",
		Hidden:    "hidden-test2",
		Invisible: "invisible-test2",
		Created:   time.Now().Add(-40 * 24 * time.Hour),
		Settings: resourcet.TestSettings{
			Foo:   7,
			Bar:   "world",
			Score: 1.2,
		},
		Authors: []resourcet.TestAuthor{
			{Name: "Charlie", Email: "charlie@example.com"},
			{Name: "Dana", Email: "dana@example.com"},
		},
		Tags:  []string{"staging"},
		Usage: types.MeterUsage[int]{Used: 30, Total: 100},
	})
	return env
}

func TestList(t *testing.T) {
	env := setupTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	empty := env.NewResource()
	resources, err := empty.List(ctx)
	require.NoError(t, err)
	assert.Len(t, resources, 2)

	var listOut bytes.Buffer
	listCmd := &ResourceListCmd[resourcet.TestResource]{
		Targets: nil,
	}
	err = listCmd.Run(ctx, testStdio(&listOut), partition)
	require.NoError(t, err)

	output := listOut.String()
	assert.Contains(t, output, "test1")
	assert.Contains(t, output, "test2")
	assert.Contains(t, output, "id-test1")
	assert.Contains(t, output, "id-test2")

	t.Run("field", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Field: xkong.GreedyStrings{"name", "id"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.NotContains(t, output, "https://example.com")

		out.Reset()
		cmd.Targets = []string{"test1"}
		err = cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output = out.String()
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("field exclude", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Field:  xkong.GreedyStrings{"-url"},
			Output: Printer{Type: PrinterTypeKeyValue},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "name:")
		assert.Contains(t, output, "id:")
		assert.NotContains(t, output, "url:")
	})

	t.Run("filter", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"name==test1"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")

		out.Reset()
		cmd.Targets = []string{"test1", "test2"}
		err = cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output = out.String()
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("filter-wildcard-nested", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"authors.*.email==alice@example.com"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "test1") // has Alice as author
		assert.NotContains(t, output, "test2")

		out.Reset()
		cmd.Filter = []string{"authors.*.email==charlie@example.com"}
		err = cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output = out.String()
		assert.Contains(t, output, "test2") // has Charlie as author
		assert.NotContains(t, output, "test1")

		out.Reset()
		cmd.Filter = []string{"authors.*.name==Bob"}
		err = cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output = out.String()
		assert.Contains(t, output, "test1") // has Bob as author
		assert.NotContains(t, output, "test2")
	})

	t.Run("filter-indexed-nested", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"authors.0.name==Alice"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "test1")    // first author is Alice
		assert.NotContains(t, output, "test2") // first author is Charlie

		out.Reset()
		cmd.Filter = []string{"authors.1.email==bob@example.com"}
		err = cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output = out.String()
		assert.Contains(t, output, "test1")    // second author is Bob
		assert.NotContains(t, output, "test2") // second author is Dana
	})

	t.Run("filter-nested-struct", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"settings.bar==hello"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "test1")    // settings.bar == "hello"
		assert.NotContains(t, output, "test2") // settings.bar == "world"
	})

	t.Run("filter-unknown-field", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"nonexistent==value"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nonexistent")

		output := out.String()
		assert.NotContains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("filter-unknown-field-dedup", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"missing_field==value"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.Error(t, err)

		// Error should mention missing_field only once, not once per resource
		errStr := err.Error()
		assert.Contains(t, errStr, "missing_field")
		count := strings.Count(errStr, "missing_field")
		assert.Equal(t, 1, count, "expected 1 occurrence, got %d", count)
	})

	t.Run("filter-unknown-nested-field", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"settings.nonexistent==value"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nonexistent")
	})

	t.Run("sort-asc", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Sort:   xkong.GreedyStrings{"name"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Equal(t, "test1\ntest2\n", output)
	})

	t.Run("sort-asc-explicit", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Sort:   xkong.GreedyStrings{"+name"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Equal(t, "test1\ntest2\n", output)
	})

	t.Run("sort-desc", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Sort:   xkong.GreedyStrings{"-name"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Equal(t, "test2\ntest1\n", output)
	})

	t.Run("sort-asc-nested", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Sort:   xkong.GreedyStrings{"settings.bar"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// test1 has settings.bar="hello", test2 has settings.bar="world"
		// ascending: "hello" < "world", so test1 comes first
		output := out.String()
		assert.Equal(t, "test1\ntest2\n", output)
	})

	t.Run("sort-desc-nested", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Sort:   xkong.GreedyStrings{"-settings.bar"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// test1 has settings.bar="hello", test2 has settings.bar="world"
		// descending: "world" > "hello", so test2 comes first
		output := out.String()
		assert.Equal(t, "test2\ntest1\n", output)
	})

	t.Run("sort-multi-field", func(t *testing.T) {
		var out bytes.Buffer
		// Both test1 and test2 have state="pending", so sort by state first,
		// then by name descending to break the tie
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Sort:   xkong.GreedyStrings{"state", "-name"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// Both have same state, so secondary sort by -name means test2 first
		output := out.String()
		assert.Equal(t, "test2\ntest1\n", output)
	})

	t.Run("sort-multi-field-mixed", func(t *testing.T) {
		var out bytes.Buffer
		// Sort by state ascending, then by settings.foo ascending
		// test1 has settings.foo=42, test2 has settings.foo=7
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Sort:   xkong.GreedyStrings{"+state", "settings.foo"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// Both have same state, secondary sort by settings.foo asc: 7 < 42, so test2 first
		output := out.String()
		assert.Equal(t, "test2\ntest1\n", output)
	})
}

func TestParseSortSpecs(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []SortSpec
		wantErr  bool
	}{
		{
			name:     "empty string",
			input:    []string{""},
			expected: []SortSpec{},
		},
		{
			name:  "single field ascending implicit",
			input: []string{"name"},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("name"), Descending: false},
			},
		},
		{
			name:  "single field ascending explicit",
			input: []string{"+name"},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("name"), Descending: false},
			},
		},
		{
			name:  "single field descending",
			input: []string{"-name"},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("name"), Descending: true},
			},
		},
		{
			name:  "nested field",
			input: []string{"timing.uptime"},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("timing.uptime"), Descending: false},
			},
		},
		{
			name:  "nested field descending",
			input: []string{"-timing.uptime"},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("timing.uptime"), Descending: true},
			},
		},
		{
			name:  "multiple fields",
			input: []string{"state", "name"},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("state"), Descending: false},
				{Path: resource.ParseFieldPath("name"), Descending: false},
			},
		},
		{
			name:  "multiple fields mixed directions",
			input: []string{"state", "-timing.uptime"},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("state"), Descending: false},
				{Path: resource.ParseFieldPath("timing.uptime"), Descending: true},
			},
		},
		{
			name:  "multiple fields with explicit prefix",
			input: []string{"+state", "-name", "+id"},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("state"), Descending: false},
				{Path: resource.ParseFieldPath("name"), Descending: true},
				{Path: resource.ParseFieldPath("id"), Descending: false},
			},
		},
		{
			name:  "with spaces",
			input: []string{" state ", " -name "},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("state"), Descending: false},
				{Path: resource.ParseFieldPath("name"), Descending: true},
			},
		},
		{
			name:    "empty field name",
			input:   []string{"-"},
			wantErr: true,
		},
		{
			name:  "empty field in list",
			input: []string{"state", "", "name"},
			expected: []SortSpec{
				{Path: resource.ParseFieldPath("state"), Descending: false},
				{Path: resource.ParseFieldPath("name"), Descending: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs, err := parseSortSpecs(tt.input...)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, specs)
		})
	}
}

func TestListOutput(t *testing.T) {
	env := setupTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	runList := func(t *testing.T, opts FormatOpts) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			FormatOpts: opts,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("table", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeTable}})
		cleaned := ansi.Strip(output)
		assert.Contains(t, cleaned, "test1")
		assert.Contains(t, cleaned, "test2")
		assert.Contains(t, cleaned, "id-test1")
	})

	t.Run("kv", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeKeyValue}})
		assert.Contains(t, output, "name:")
		assert.Contains(t, output, "id:")
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "test2")
	})

	t.Run("json", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeJSON}})
		var resources []map[string]any
		err := json.Unmarshal([]byte(output), &resources)
		require.NoError(t, err)
		require.Len(t, resources, 2)

		names := map[string]bool{}
		for _, res := range resources {
			if name, ok := res["name"].(string); ok {
				names[name] = true
			}
		}
		assert.True(t, names["test1"])
		assert.True(t, names["test2"])
	})

	t.Run("yaml", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeYAML}})
		var resources []map[string]any
		err := yaml.Unmarshal([]byte(output), &resources)
		require.NoError(t, err)
		require.Len(t, resources, 2)

		names := map[string]bool{}
		for _, res := range resources {
			if name, ok := res["name"].(string); ok {
				names[name] = true
			}
		}
		assert.True(t, names["test1"])
		assert.True(t, names["test2"])
	})

	t.Run("json field", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeJSON}, Field: xkong.GreedyStrings{"id"}})
		var resources []map[string]any
		err := json.Unmarshal([]byte(output), &resources)
		require.NoError(t, err)
		require.Len(t, resources, 2)

		for _, res := range resources {
			assert.Contains(t, res, "id")
			assert.NotContains(t, res, "name")
		}
	})

	t.Run("yaml field", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeYAML}, Field: xkong.GreedyStrings{"id"}})
		var resources []map[string]any
		err := yaml.Unmarshal([]byte(output), &resources)
		require.NoError(t, err)
		require.Len(t, resources, 2)

		for _, res := range resources {
			assert.Contains(t, res, "id")
			assert.NotContains(t, res, "name")
		}
	})

	t.Run("raw", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeRaw}})
		var resources []resourcet.TestResource
		err := json.Unmarshal([]byte(output), &resources)
		require.NoError(t, err)
		require.Len(t, resources, 2)

		names := map[string]bool{}
		for _, res := range resources {
			names[res.Name] = true
		}
		assert.True(t, names["test1"])
		assert.True(t, names["test2"])
	})

	t.Run("quiet", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeQuiet}})
		assert.Equal(t, "test1\ntest2\n", output)
	})

	t.Run("quiet field", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeQuiet}, Field: xkong.GreedyStrings{"id", "url"}})
		assert.Equal(t, "id-test1 https://example.com\nid-test2 https://example.org\n", output)
	})

	t.Run("template", func(t *testing.T) {
		output := runList(t, FormatOpts{Output: Printer{Type: PrinterTypeTemplate, Value: "{{.name}}-{{.id}}"}})
		assert.Equal(t, "test1-id-test1\ntest2-id-test2\n", output)
	})
}

func TestPartialResultsPrintedBeforeError(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		env := setupTestEnv()
		env.Hooks.List = func(ctx context.Context, next func(context.Context) ([]resource.Resource, error)) ([]resource.Resource, error) {
			resources, _ := next(ctx)
			return resources, errors.New("list failed")
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), nil)
		require.Error(t, err)
		assert.Contains(t, out.String(), "test1")
		assert.Contains(t, out.String(), "test2")
	})

	t.Run("get", func(t *testing.T) {
		env := setupTestEnv()
		env.Hooks.Get = func(ctx context.Context, keys []string, next func(context.Context, []string) ([]resource.Resource, error)) ([]resource.Resource, error) {
			resources, _ := next(ctx, keys)
			return resources, errors.New("get failed")
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Targets: []string{"test1", "missing"},
			Output:  Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), nil)
		require.Error(t, err)
		assert.Contains(t, out.String(), "test1")
		assert.NotContains(t, out.String(), "missing")
	})

	t.Run("create", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Hooks.Create = func(ctx context.Context, fields []resource.Field, next func(context.Context, []resource.Field) ([]resource.Resource, error)) ([]resource.Resource, error) {
			resources, _ := next(ctx, fields)
			return resources, errors.New("create failed")
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceCreateCmd[resourcet.TestResource]{
			Set:    []map[string]string{{"name": "created"}},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), nil)
		require.Error(t, err)
		assert.Contains(t, out.String(), "created")
	})

	t.Run("delete", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{Name: "ok", ID: "id-ok"})
		env.Add(resourcet.TestResource{Name: "fail", ID: "id-fail"})
		env.Hooks.Delete = func(ctx context.Context, _ []string, _ func(context.Context, []string) error) error {
			return group.ErrRefNotFound{Refs: group.Refs{{Name: "fail"}}}
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceRemoveCmd[resourcet.TestResource]{
			Targets: []string{"ok", "fail"},
			Output:  Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), nil)
		require.Error(t, err)
		output := out.String()
		assert.Contains(t, output, "ok")
		assert.NotContains(t, output, "fail")
	})
}

func TestPartialResultsOrderWhenCallerPrintsError(t *testing.T) {
	t.Run("list/table", func(t *testing.T) {
		env := setupTestEnv()
		env.Hooks.List = func(ctx context.Context, next func(context.Context) ([]resource.Resource, error)) ([]resource.Resource, error) {
			resources, _ := next(ctx)
			return resources, errors.New("list failed")
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Output: Printer{Type: PrinterTypeTable},
		}
		err := cmd.Run(ctx, testStdio(&out), nil)
		require.Error(t, err)
		fmt.Fprintf(&out, "error: %v\n", err)
		output := out.String()
		idxOK := strings.Index(output, "test1")
		idxErr := strings.Index(output, "error:")
		require.NotEqual(t, -1, idxOK)
		require.NotEqual(t, -1, idxErr)
		assert.Less(t, idxOK, idxErr)
	})

	t.Run("get/kv", func(t *testing.T) {
		env := setupTestEnv()
		env.Hooks.Get = func(ctx context.Context, keys []string, next func(context.Context, []string) ([]resource.Resource, error)) ([]resource.Resource, error) {
			resources, _ := next(ctx, keys)
			return resources, errors.New("get failed")
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Targets: []string{"test1", "missing"},
			Output:  Printer{Type: PrinterTypeKeyValue},
		}
		err := cmd.Run(ctx, testStdio(&out), nil)
		require.Error(t, err)
		fmt.Fprintf(&out, "error: %v\n", err)
		output := out.String()
		idxName := strings.Index(output, "name:")
		idxOK := strings.Index(output, "test1")
		idxErr := strings.Index(output, "error:")
		require.NotEqual(t, -1, idxName)
		require.NotEqual(t, -1, idxOK)
		require.NotEqual(t, -1, idxErr)
		assert.Less(t, idxName, idxErr)
		assert.Less(t, idxOK, idxErr)
	})

	t.Run("delete/quiet", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{Name: "ok", ID: "id-ok"})
		env.Add(resourcet.TestResource{Name: "fail", ID: "id-fail"})
		env.Hooks.Delete = func(ctx context.Context, _ []string, _ func(context.Context, []string) error) error {
			return group.ErrRefNotFound{Refs: group.Refs{{Name: "fail"}}}
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceRemoveCmd[resourcet.TestResource]{
			Targets: []string{"ok", "fail"},
			Output:  Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), nil)
		require.Error(t, err)
		fmt.Fprintf(&out, "error: %v\n", err)
		output := out.String()
		idxOK := strings.Index(output, "ok")
		idxErr := strings.Index(output, "error:")
		require.NotEqual(t, -1, idxOK)
		require.NotEqual(t, -1, idxErr)
		success := output[:idxErr]
		assert.Contains(t, success, "ok")
		assert.NotContains(t, success, "fail")
		assert.Less(t, idxOK, idxErr)
	})

	t.Run("create/kv", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Hooks.Create = func(ctx context.Context, fields []resource.Field, next func(context.Context, []resource.Field) ([]resource.Resource, error)) ([]resource.Resource, error) {
			resources, _ := next(ctx, fields)
			return resources, errors.New("create failed")
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceCreateCmd[resourcet.TestResource]{
			Set:    []map[string]string{{"name": "created"}},
			Output: Printer{Type: PrinterTypeKeyValue},
		}
		err := cmd.Run(ctx, testStdio(&out), nil)
		require.Error(t, err)
		fmt.Fprintf(&out, "error: %v\n", err)
		output := out.String()
		idxName := strings.Index(output, "name:")
		idxCreated := strings.Index(output, "created")
		idxErr := strings.Index(output, "error:")
		require.NotEqual(t, -1, idxName)
		require.NotEqual(t, -1, idxCreated)
		require.NotEqual(t, -1, idxErr)
		assert.Less(t, idxName, idxErr)
		assert.Less(t, idxCreated, idxErr)
	})
}

func TestTableNestedFieldSelection(t *testing.T) {
	env := setupTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	var out bytes.Buffer
	cmd := &ResourceGetCmd[resourcet.TestResource]{
		Targets: []string{"test1"},
		Output:  Printer{Type: PrinterTypeTable},
		Field:   xkong.GreedyStrings{"name", "authors"},
	}
	err := cmd.Run(ctx, testStdio(&out), partition)
	require.NoError(t, err)

	cleaned := ansi.Strip(out.String())
	assert.Contains(t, cleaned, "Alice")
	assert.Contains(t, cleaned, "alice@example.com")
}

func TestGet(t *testing.T) {
	env := setupTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	empty := env.NewResource()
	resources, err := empty.Get(ctx, []string{"test1"})
	require.NoError(t, err)
	require.Len(t, resources, 1)

	test := resources[0].(resourcet.TestResource)
	assert.Equal(t, "test1", test.Name)
	assert.Equal(t, "id-test1", test.ID)
	assert.Equal(t, 42, test.Settings.Foo)
	assert.Equal(t, "hello", test.Settings.Bar)

	fields, err := test.Fields(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, fields)

	var inspectOut bytes.Buffer
	inspectCmd := &ResourceGetCmd[resourcet.TestResource]{
		Targets: []string{"test1"},
	}
	err = inspectCmd.Run(ctx, testStdio(&inspectOut), partition)
	require.NoError(t, err)

	output := inspectOut.String()
	assert.Contains(t, output, "test1")
	assert.Contains(t, output, "id-test1")
	assert.Contains(t, output, "https://example.com")

	t.Run("no_args", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Targets: []string{},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.Error(t, err)
	})

	t.Run("multiple", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Targets: []string{"test1", "test2"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "test2")
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "id-test2")
	})

	t.Run("field", func(t *testing.T) {
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Targets: []string{"test1"},
			Field:   xkong.GreedyStrings{"id", "url"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "https://example.com")

		out.Reset()
		cmd.Targets = []string{"test1", "test2"}
		err = cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output = out.String()
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "https://example.com")
		assert.Contains(t, output, "id-test2")
		assert.Contains(t, output, "https://example.org")
	})
}

func TestFieldVerbosity(t *testing.T) {
	env := setupTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	runList := func(t *testing.T, fields []string) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Field: fields,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)
		return out.String()
	}

	runInspect := func(t *testing.T, fields []string) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Targets: []string{"test1"},
			Field:   fields,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("list_short_fields", func(t *testing.T) {
		output := ansi.Strip(runList(t, nil))
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.NotContains(t, output, "hello")
		assert.NotContains(t, output, "hidden-test1")
		assert.NotContains(t, output, "invisible-test1")
	})

	t.Run("inspect_short_long_fields", func(t *testing.T) {
		output := runInspect(t, nil)
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "hello")
		assert.NotContains(t, output, "hidden-test1")
		assert.NotContains(t, output, "invisible-test1")
	})

	t.Run("inspect_hidden_fields", func(t *testing.T) {
		output := runInspect(t, []string{"hidden"})
		assert.Contains(t, output, "hidden-test1")
		assert.NotContains(t, output, "invisible-test1")
	})

	t.Run("inspect_all_fields", func(t *testing.T) {
		output := runInspect(t, []string{"all"})
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "hello")
		assert.Contains(t, output, "hidden-test1")
		assert.NotContains(t, output, "invisible-test1")
	})

	t.Run("inspect_invisible_fields", func(t *testing.T) {
		output := runInspect(t, []string{"invisible"})
		assert.NotContains(t, output, "invisible-test1")
	})
}

func TestGetOutput(t *testing.T) {
	env := setupTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	runInspect := func(t *testing.T, printer Printer) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Targets: []string{"test1", "test2"},
			Output:  printer,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runInspect(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test1\ntest2\n", output)
	})

	// Other formats are covered in TestListOutput.
}

func TestWait(t *testing.T) {
	partition := &resource.Partition{}

	t.Run("already_matching", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{
			ID:    "id-test1",
			Name:  "test1",
			State: "ready",
		})
		env.Add(resourcet.TestResource{
			ID:    "id-test2",
			Name:  "test2",
			State: "ready",
		})
		ctx := resourcet.WithTestEnv(context.Background(), env)

		cmd := &ResourceWaitCmd[resourcet.TestResource]{
			Targets:  []string{"test1", "test2"},
			Until:    []string{"state==ready"},
			Interval: 10 * time.Millisecond,
		}
		err := cmd.Run(ctx, testStdio(&bytes.Buffer{}), partition)
		require.NoError(t, err)
	})

	t.Run("timeout", func(t *testing.T) {
		env := setupTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)
		ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
		defer cancel()

		cmd := &ResourceWaitCmd[resourcet.TestResource]{
			Targets:  []string{"test1", "test2"},
			Until:    []string{"state==ready"},
			Interval: 10 * time.Millisecond,
		}
		err := cmd.Run(ctx, testStdio(&bytes.Buffer{}), partition)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestWaitOutput(t *testing.T) {
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{
		ID:    "id-test1",
		Name:  "test1",
		State: "ready",
	})
	env.Add(resourcet.TestResource{
		ID:    "id-test2",
		Name:  "test2",
		State: "ready",
	})
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	runWait := func(t *testing.T, printer Printer) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceWaitCmd[resourcet.TestResource]{
			Targets:  []string{"test1", "test2"},
			Until:    []string{"state==ready"},
			Interval: 10 * time.Millisecond,
			Output:   printer,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runWait(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test1\ntest2\n", output)
	})

	// Other formats are covered in TestListOutput.
}

func TestCreate(t *testing.T) {
	env := resourcet.NewTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)

	empty := env.NewResource()
	templateFields, err := empty.Fields(ctx)
	require.NoError(t, err)

	for key, field := range resource.IterFields(templateFields) {
		if field.Create == nil {
			continue
		}
		switch key.String() {
		case "name":
			field.Create.Set = "test-new"
		case "settings.foo":
			field.Create.Set = 100
		case "settings.bar":
			field.Create.Set = "created"
		}
	}

	res, err := empty.Create(ctx, templateFields)
	require.NoError(t, err)
	require.Len(t, res, 1)

	created := res[0].(resourcet.TestResource)
	assert.Equal(t, "test-new", created.Name)
	assert.Equal(t, 100, created.Settings.Foo)
	assert.Equal(t, "created", created.Settings.Bar)
	assert.Contains(t, env.Store, "test-new")
}

func TestCreateOutput(t *testing.T) {
	env := resourcet.NewTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	runCreate := func(t *testing.T, printer Printer) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceCreateCmd[resourcet.TestResource]{
			Set: []map[string]string{
				{"name": "test-output"},
				{"settings.foo": "100"},
				{"settings.bar": "created"},
			},
			Output: printer,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runCreate(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test-output\n", output)
	})

	// Other formats are covered in TestListOutput.
}

func TestCreateDryRun(t *testing.T) {
	env := resourcet.NewTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	var out bytes.Buffer
	cmd := &ResourceCreateCmd[resourcet.TestResource]{
		DryRun: true,
		Set: []map[string]string{
			{"name": "test-dry"},
			{"settings.foo": "100"},
			{"settings.bar": "created"},
		},
	}
	err := cmd.Run(ctx, testStdio(&out), partition)
	require.NoError(t, err)

	assert.NotContains(t, env.Store, "test-dry")

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)

	expected := [][]string{
		{"name", ":=", "test-dry"},
		{"settings.foo", ":=", "100"},
		{"settings.bar", ":=", "created"},
	}
	for i, expectedFields := range expected {
		assert.Equal(t, expectedFields, strings.Fields(lines[i]))
	}
}

func TestCreateDryRunWithShortcutZeroValues(t *testing.T) {
	// Test that shortcut flags correctly handle zero values when explicitly set.
	// This validates that we use flag.Set instead of IsZero() to determine
	// whether a flag was provided.
	//
	// The test uses a Kong CLI struct with shortcut tags and calls
	// ApplyShortcutFlags to populate SetArgs, mirroring how real commands work.

	// shortcutCLI is a Kong-parseable CLI struct that mirrors shortcut flags
	// for TestResource create fields (settings.foo and settings.bar).
	type shortcutCLI struct {
		Foo int    `name:"foo" shortcut:"settings.foo"`
		Bar string `name:"bar" shortcut:"settings.bar"`
	}

	// applyShortcuts parses cliArgs via Kong, applies shortcut flags into
	// baseArgs, and returns the resulting SetArgs.
	applyShortcuts := func(t *testing.T, baseArgs SetArgs, cliArgs ...string) SetArgs {
		t.Helper()
		cli := &shortcutCLI{}
		flags := parseFlags(t, cli, cliArgs...)
		require.NoError(t, ApplyShortcutFlags(&baseArgs, flags))
		return baseArgs
	}

	t.Run("zero int value is applied when explicitly set", func(t *testing.T) {
		setArgs := applyShortcuts(t,
			SetArgs{Set: []map[string]string{{"name": "test-zero"}}},
			"--foo=0",
		)

		env := resourcet.NewTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)
		partition := &resource.Partition{}

		var out bytes.Buffer
		cmd := &ResourceCreateCmd[resourcet.TestResource]{
			DryRun:  true,
			SetArgs: setArgs,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// Resource should not be created (dry-run)
		assert.NotContains(t, env.Store, "test-zero")

		// Output should show the zero value being set
		output := out.String()
		assert.Contains(t, output, "settings.foo")
		assert.Contains(t, output, "0")
	})

	t.Run("empty string value is applied when explicitly set", func(t *testing.T) {
		setArgs := applyShortcuts(t,
			SetArgs{Set: []map[string]string{{"name": "test-empty"}}},
			"--bar=",
		)

		env := resourcet.NewTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)
		partition := &resource.Partition{}

		var out bytes.Buffer
		cmd := &ResourceCreateCmd[resourcet.TestResource]{
			DryRun:  true,
			SetArgs: setArgs,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// Resource should not be created (dry-run)
		assert.NotContains(t, env.Store, "test-empty")

		// Output should show the empty string being set
		output := out.String()
		assert.Contains(t, output, "settings.bar")
	})

	t.Run("unprovided fields are not included in patches", func(t *testing.T) {
		// Neither --foo nor --bar provided; shortcut flags should not add them.
		setArgs := applyShortcuts(t,
			SetArgs{Set: []map[string]string{{"name": "test-minimal"}}},
		)

		env := resourcet.NewTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)
		partition := &resource.Partition{}

		var out bytes.Buffer
		cmd := &ResourceCreateCmd[resourcet.TestResource]{
			DryRun:  true,
			SetArgs: setArgs,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// Resource should not be created (dry-run)
		assert.NotContains(t, env.Store, "test-minimal")

		// Output should only show the name, not settings.foo
		output := out.String()
		assert.Contains(t, output, "name")
		assert.NotContains(t, output, "settings.foo")
	})
}

func TestCreatePatchSpecFileArgs(t *testing.T) {
	nameFile := tempFile(t, " test-file ")
	setFile := tempFile(t, " 123 \n")
	setTextFile := tempFile(t, " created\n")

	cmd := &ResourceCreateCmd[resourcet.TestResource]{
		Set: []map[string]string{{"name": "test-inline"}},
		SetFile: []map[string]string{
			{"name": nameFile},
			{"settings.foo": setFile},
			{"settings.bar": setTextFile},
		},
	}

	spec, err := cmd.toPatchSpec()
	require.NoError(t, err)

	assert.Equal(t, []string{"test-inline", "test-file"}, spec.Set["name"])
	assert.Equal(t, []string{"123"}, spec.Set["settings.foo"])
	assert.Equal(t, []string{"created"}, spec.Set["settings.bar"])
}

func TestCreateSetFile(t *testing.T) {
	env := resourcet.NewTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	nameFile := tempFile(t, " test-file ")
	setFile := tempFile(t, " 101 \n")
	setTextFile := tempFile(t, " created\n")

	var out bytes.Buffer
	cmd := &ResourceCreateCmd[resourcet.TestResource]{
		SetFile: []map[string]string{
			{"name": nameFile},
			{"settings.foo": setFile},
			{"settings.bar": setTextFile},
		},
	}

	err := cmd.Run(ctx, testStdio(&out), partition)
	require.NoError(t, err)

	created, ok := env.Store["test-file"]
	require.True(t, ok)
	assert.Equal(t, 101, created.Settings.Foo)
	assert.Equal(t, "created", created.Settings.Bar)
}

func TestEdit(t *testing.T) {
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{
		ID:   "id-edit",
		Name: "test-edit",
		URL:  "https://example.com",
		Settings: resourcet.TestSettings{
			Foo: 10,
			Bar: "original",
		},
	})
	ctx := resourcet.WithTestEnv(context.Background(), env)

	empty := env.NewResource()
	resources, err := empty.Get(ctx, []string{"test-edit"})
	require.NoError(t, err)
	require.Len(t, resources, 1)

	target := resources[0]
	templateFields, err := target.Fields(ctx)
	require.NoError(t, err)

	for key, field := range resource.IterFields(templateFields) {
		if field.Edit == nil {
			continue
		}
		switch key.String() {
		case "settings.foo":
			field.Edit.Set = 999
		case "settings.bar":
			field.Edit.Set = "modified"
		}
	}

	err = empty.Edit(ctx, target.Key().String(), templateFields)
	require.NoError(t, err)

	results, err := empty.Get(ctx, []string{target.Key().String()})
	require.NoError(t, err)
	require.Len(t, results, 1)

	edited := results[0].(resourcet.TestResource)
	assert.Equal(t, "test-edit", edited.Name)
	assert.Equal(t, "id-edit", edited.ID)
	assert.Equal(t, 999, edited.Settings.Foo)
	assert.Equal(t, "modified", edited.Settings.Bar)

	stored := env.Store["test-edit"]
	assert.Equal(t, 999, stored.Settings.Foo)
	assert.Equal(t, "modified", stored.Settings.Bar)
}

func TestEditOutput(t *testing.T) {
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{
		ID:   "id-edit",
		Name: "test-edit",
		URL:  "https://example.com",
		Settings: resourcet.TestSettings{
			Foo: 10,
			Bar: "original",
		},
	})
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	runEdit := func(t *testing.T, printer Printer) string {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceEditCmd[resourcet.TestResource]{
			Target: "test-edit",
			Set: []map[string]string{
				{"settings.foo": "999"},
			},
			Output: printer,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runEdit(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test-edit\n", output)
	})

	// Other formats are covered in TestListOutput.
}

func TestEditDryRun(t *testing.T) {
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{
		ID:   "id-edit",
		Name: "test-edit",
		URL:  "https://example.com",
		Settings: resourcet.TestSettings{
			Foo: 10,
			Bar: "original",
		},
	})
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	var out bytes.Buffer
	cmd := &ResourceEditCmd[resourcet.TestResource]{
		Target: "test-edit",
		DryRun: true,
		Set: []map[string]string{
			{"settings.foo": "999"},
			{"settings.bar": "modified"},
		},
	}
	err := cmd.Run(ctx, testStdio(&out), partition)
	require.NoError(t, err)

	stored := env.Store["test-edit"]
	assert.Equal(t, 10, stored.Settings.Foo)
	assert.Equal(t, "original", stored.Settings.Bar)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 2)

	expected := [][]string{
		{"settings.foo", ":=", "999"},
		{"settings.bar", ":=", "modified"},
	}
	for i, expectedFields := range expected {
		assert.Equal(t, expectedFields, strings.Fields(lines[i]))
	}
}

func TestEditCmdNoChangesDryRun(t *testing.T) {
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{
		ID:   "id-edit",
		Name: "test-edit",
		URL:  "https://example.com",
		Settings: resourcet.TestSettings{
			Foo: 10,
			Bar: "original",
		},
	})
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	var out bytes.Buffer
	cmd := &ResourceEditCmd[resourcet.TestResource]{
		Target: "test-edit",
		Cmd:    "cat", // pass through unchanged
		DryRun: true,
	}
	err := cmd.Run(ctx, testStdio(&out), partition)
	require.NoError(t, err)

	// No changes should produce no output
	assert.Empty(t, strings.TrimSpace(out.String()), "dry-run with no changes should produce empty output")

	// Resource should be unchanged
	stored := env.Store["test-edit"]
	assert.Equal(t, 10, stored.Settings.Foo)
	assert.Equal(t, "original", stored.Settings.Bar)
}

func TestEditCmdWithChangesDryRun(t *testing.T) {
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{
		ID:   "id-edit",
		Name: "test-edit",
		URL:  "https://example.com",
		Settings: resourcet.TestSettings{
			Foo: 10,
			Bar: "old-value", // maps to field name "settings.bar"
		},
	})
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	var out bytes.Buffer
	cmd := &ResourceEditCmd[resourcet.TestResource]{
		Target: "test-edit",
		Cmd:    `sed 's/old-value/new-value/'`, // change settings.bar
		DryRun: true,
	}
	err := cmd.Run(ctx, testStdio(&out), partition)
	require.NoError(t, err)

	// Should have output showing the change
	output := strings.TrimSpace(out.String())
	t.Logf("Output: %q", output)
	assert.Contains(t, output, "settings.bar", "dry-run with changes should show changed field")
	assert.Contains(t, output, "new-value", "dry-run with changes should show new value")

	// Resource should be unchanged (dry-run)
	stored := env.Store["test-edit"]
	assert.Equal(t, "old-value", stored.Settings.Bar)
}

func TestEditPatchSpecFileArgs(t *testing.T) {
	setFile := tempFile(t, " 123 \n")
	addFile := tempFile(t, " new-entry\n")
	delFile := tempFile(t, " old-entry\n")

	cmd := &ResourceEditCmd[resourcet.TestResource]{
		Set:     []map[string]string{{"settings.bar": "inline"}},
		SetFile: []map[string]string{{"settings.foo": setFile}},
		Add:     []map[string]string{{"authors": "inline-entry"}},
		AddFile: []map[string]string{{"authors": addFile}},
		Del:     []map[string]string{{"url": "inline-entry"}},
		DelFile: []map[string]string{{"url": delFile}},
	}

	spec, err := cmd.toPatchSpec()
	require.NoError(t, err)

	assert.Equal(t, []string{"inline"}, spec.Set["settings.bar"])
	assert.Equal(t, []string{"123"}, spec.Set["settings.foo"])
	assert.Equal(t, []string{"inline-entry", "new-entry"}, spec.Add["authors"])
	assert.Equal(t, []string{"inline-entry", "old-entry"}, spec.Del["url"])
}

func TestDelete(t *testing.T) {
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{
		ID:   "id-delete",
		Name: "test-delete",
		URL:  "https://example.com",
	})
	env.Add(resourcet.TestResource{
		ID:   "id-keep",
		Name: "test-keep",
		URL:  "https://example.org",
	})
	ctx := resourcet.WithTestEnv(context.Background(), env)

	empty := env.NewResource()
	resources, err := empty.Get(ctx, []string{"test-delete"})
	require.NoError(t, err)
	require.Len(t, resources, 1)

	err = empty.Delete(ctx, []string{"test-delete"})
	require.NoError(t, err)

	assert.NotContains(t, env.Store, "test-delete")
	assert.Contains(t, env.Store, "test-keep")

	resources, err = empty.Get(ctx, []string{"test-delete"})
	require.NoError(t, err)
	assert.Empty(t, resources)
}

func TestRemoveOutput(t *testing.T) {
	partition := &resource.Partition{}

	t.Run("no_args", func(t *testing.T) {
		env := setupTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceRemoveCmd[resourcet.TestResource]{}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.Error(t, err)
	})

	runRemove := func(t *testing.T, printer Printer) string {
		t.Helper()
		env := setupTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceRemoveCmd[resourcet.TestResource]{
			Targets: []string{"test1", "test2"},
			Output:  printer,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)
		return out.String()
	}

	t.Run("quiet", func(t *testing.T) {
		output := runRemove(t, Printer{Type: PrinterTypeQuiet})
		assert.Equal(t, "test1\ntest2\n", output)
	})

	t.Run("kv", func(t *testing.T) {
		output := runRemove(t, Printer{Type: PrinterTypeKeyValue})
		assert.Contains(t, output, "name:")
		assert.Contains(t, output, "id:")
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "id-test1")
		assert.Contains(t, output, "test2")
	})

	// Other formats are covered in TestListOutput.
}

func tempFile(t *testing.T, contents string) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "set-file-*")
	require.NoError(t, err)

	_, err = file.WriteString(contents)
	require.NoError(t, err)

	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)

	return file.Name()
}

func testStdio(out io.Writer) config.Stdio {
	return config.Stdio{
		Stdin:  &bytes.Buffer{},
		Stdout: out,
		Stderr: out,
	}
}

func testStdioWithInput(out io.Writer, in io.Reader) config.Stdio {
	return config.Stdio{
		Stdin:  in,
		Stdout: out,
		Stderr: out,
	}
}

func TestValueCallback(t *testing.T) {
	partition := &resource.Partition{}

	t.Run("list_without_lazy_field", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "1", Name: "res1", State: "running"})
		env.Add(resourcet.TestResource{ID: "2", Name: "res2", State: "stopped"})
		var resolvedMu sync.Mutex
		var resolved []string
		env.Hooks.OnLazyResolve = func(resourceName string) {
			resolvedMu.Lock()
			resolved = append(resolved, resourceName)
			resolvedMu.Unlock()
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "res1")
		assert.Contains(t, output, "running")
		assert.NotContains(t, output, "computed-")
		// Callback should not be invoked when lazy field is not requested
		resolvedMu.Lock()
		resolvedCopy := slices.Clone(resolved)
		resolvedMu.Unlock()
		assert.Empty(t, resolvedCopy, "callbacks should not be invoked when lazy field not selected")
	})

	t.Run("list_with_lazy_field", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "1", Name: "res1", State: "running"})
		env.Add(resourcet.TestResource{ID: "2", Name: "res2", State: "stopped"})
		var resolvedMu sync.Mutex
		var resolved []string
		env.Hooks.OnLazyResolve = func(resourceName string) {
			resolvedMu.Lock()
			resolved = append(resolved, resourceName)
			resolvedMu.Unlock()
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Field: xkong.GreedyStrings{"+lazy"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "res1")
		assert.Contains(t, output, "computed-res1")
		assert.Contains(t, output, "res2")
		assert.Contains(t, output, "computed-res2")
		// Callback should be invoked once per resource
		resolvedMu.Lock()
		resolvedCopy := slices.Clone(resolved)
		resolvedMu.Unlock()
		assert.ElementsMatch(t, []string{"res1", "res2"}, resolvedCopy, "callbacks should be invoked for each resource")
	})

	t.Run("get_with_lazy_field", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "1", Name: "res1", State: "running"})
		env.Add(resourcet.TestResource{ID: "2", Name: "res2", State: "stopped"})
		var resolvedMu sync.Mutex
		var resolved []string
		env.Hooks.OnLazyResolve = func(resourceName string) {
			resolvedMu.Lock()
			resolved = append(resolved, resourceName)
			resolvedMu.Unlock()
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceGetCmd[resourcet.TestResource]{
			Targets: []string{"res1"},
			Field:   xkong.GreedyStrings{"+lazy"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "res1")
		assert.Contains(t, output, "computed-res1")
		resolvedMu.Lock()
		resolvedCopy := slices.Clone(resolved)
		resolvedMu.Unlock()
		assert.ElementsMatch(t, []string{"res1"}, resolvedCopy, "callback should be invoked once for requested resource")
	})

	t.Run("quiet_output_with_lazy_field", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "1", Name: "res1", State: "running"})
		env.Add(resourcet.TestResource{ID: "2", Name: "res2", State: "stopped"})
		var resolvedMu sync.Mutex
		var resolved []string
		env.Hooks.OnLazyResolve = func(resourceName string) {
			resolvedMu.Lock()
			resolved = append(resolved, resourceName)
			resolvedMu.Unlock()
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Output: Printer{Type: PrinterTypeQuiet},
			Field:  xkong.GreedyStrings{"name", "lazy"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "res1 computed-res1")
		assert.Contains(t, output, "res2 computed-res2")
		resolvedMu.Lock()
		resolvedCopy := slices.Clone(resolved)
		resolvedMu.Unlock()
		assert.ElementsMatch(t, []string{"res1", "res2"}, resolvedCopy)
	})

	t.Run("filter_on_lazy_field_without_selecting_it", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "1", Name: "res1", State: "running"})
		env.Add(resourcet.TestResource{ID: "2", Name: "res2", State: "stopped"})
		var resolvedMu sync.Mutex
		var resolved []string
		env.Hooks.OnLazyResolve = func(resourceName string) {
			resolvedMu.Lock()
			resolved = append(resolved, resourceName)
			resolvedMu.Unlock()
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			// Filter on lazy field, but don't select it for output
			Filter: []string{"lazy==computed-res1"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		// Should only show res1 (filtered by lazy field)
		assert.Contains(t, output, "res1")
		assert.NotContains(t, output, "res2")
		// Callbacks should be invoked to evaluate the filter for all resources
		resolvedMu.Lock()
		resolvedCopy := slices.Clone(resolved)
		resolvedMu.Unlock()
		assert.ElementsMatch(t, []string{"res1", "res2"}, resolvedCopy, "callbacks should be invoked to evaluate filter")
	})

	t.Run("filter_and_select_lazy_field", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "1", Name: "res1", State: "running"})
		env.Add(resourcet.TestResource{ID: "2", Name: "res2", State: "stopped"})
		var resolvedMu sync.Mutex
		var resolved []string
		env.Hooks.OnLazyResolve = func(resourceName string) {
			resolvedMu.Lock()
			resolved = append(resolved, resourceName)
			resolvedMu.Unlock()
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			// Filter on lazy field AND select it for output
			Filter: []string{"lazy==computed-res1"},
			Field:  xkong.GreedyStrings{"+lazy"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		// Should only show res1 (filtered by lazy field)
		assert.Contains(t, output, "res1")
		assert.Contains(t, output, "computed-res1")
		assert.NotContains(t, output, "res2")
		// Callback should only be invoked once per resource, not twice
		// (once for filtering, once for display would be wrong)
		resolvedMu.Lock()
		resolvedCopy := slices.Clone(resolved)
		resolvedMu.Unlock()
		assert.ElementsMatch(t, []string{"res1", "res2"}, resolvedCopy, "callbacks should be invoked once per resource, not twice")
	})

	t.Run("list_filter_sort_select_lazy_once", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "1", Name: "res1", State: "running"})
		env.Add(resourcet.TestResource{ID: "2", Name: "res2", State: "stopped"})
		var resolvedMu sync.Mutex
		var resolved []string
		env.Hooks.OnLazyResolve = func(resourceName string) {
			resolvedMu.Lock()
			resolved = append(resolved, resourceName)
			resolvedMu.Unlock()
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"lazy==computed-res1"},
			Sort:   xkong.GreedyStrings{"lazy"},
			Output: Printer{Type: PrinterTypeQuiet},
			Field:  xkong.GreedyStrings{"name", "lazy"},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "res1")
		assert.Contains(t, output, "computed-res1")
		assert.NotContains(t, output, "res2")
		resolvedMu.Lock()
		resolvedCopy := slices.Clone(resolved)
		resolvedMu.Unlock()
		assert.ElementsMatch(t, []string{"res1", "res2"}, resolvedCopy, "callback should be invoked once per resource")
	})
}

func TestDeleteBulk(t *testing.T) {
	partition := &resource.Partition{}

	t.Run("all_with_confirmation", func(t *testing.T) {
		env := setupTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		in := strings.NewReader("YES\n")
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			All: true,
		}
		err := cmd.Run(ctx, testStdioWithInput(&out, in), partition)
		require.NoError(t, err)

		// All resources should be deleted
		assert.Empty(t, env.Store)

		output := out.String()
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "test2")
	})

	t.Run("all_with_force", func(t *testing.T) {
		env := setupTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			All:   true,
			Force: true,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// All resources should be deleted
		assert.Empty(t, env.Store)
	})

	t.Run("all_cancelled", func(t *testing.T) {
		env := setupTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		in := strings.NewReader("no\n")
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			All: true,
		}
		err := cmd.Run(ctx, testStdioWithInput(&out, in), partition)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cancelled")

		// Resources should not be deleted
		assert.Len(t, env.Store, 2)
	})

	t.Run("empty", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			All:   true,
			Force: true,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		output := out.String()
		assert.NotContains(t, output, "test1")
	})

	t.Run("filter_with_force", func(t *testing.T) {
		env := setupTestEnv()
		env.Store["test1"] = resourcet.TestResource{
			ID:    env.Store["test1"].ID,
			Name:  env.Store["test1"].Name,
			State: "running",
			URL:   env.Store["test1"].URL,
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			Filter: []string{"state==running"},
			Force:  true,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// Only test1 should be deleted (matches filter)
		assert.NotContains(t, env.Store, "test1")
		// test2 should still exist (doesn't match filter)
		assert.Contains(t, env.Store, "test2")
	})

	t.Run("filter_no_match", func(t *testing.T) {
		env := setupTestEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			Filter: []string{"state==nonexistent"},
			Force:  true,
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)

		// No resources should be deleted (no matches)
		assert.Len(t, env.Store, 2)
	})

	t.Run("filter_with_confirmation", func(t *testing.T) {
		env := setupTestEnv()
		env.Store["test1"] = resourcet.TestResource{
			ID:    env.Store["test1"].ID,
			Name:  env.Store["test1"].Name,
			State: "running",
			URL:   env.Store["test1"].URL,
		}
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		in := strings.NewReader("YES\n")
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			Filter: []string{"state==running"},
		}
		err := cmd.Run(ctx, testStdioWithInput(&out, in), partition)
		require.NoError(t, err)

		// Only test1 should be deleted
		assert.NotContains(t, env.Store, "test1")
		assert.Contains(t, env.Store, "test2")
	})
}

func TestFilterComparisonOperators(t *testing.T) {
	env := setupTestEnv()
	ctx := resourcet.WithTestEnv(context.Background(), env)
	partition := &resource.Partition{}

	runFilter := func(t *testing.T, filter string) (string, error) {
		t.Helper()
		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{filter},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		return out.String(), err
	}

	t.Run("equal", func(t *testing.T) {
		output, err := runFilter(t, "settings.foo==7")
		require.NoError(t, err)
		assert.Contains(t, output, "test2")
		assert.NotContains(t, output, "test1")
	})

	t.Run("greater", func(t *testing.T) {
		output, err := runFilter(t, "settings.foo>7")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("greater_equal", func(t *testing.T) {
		output, err := runFilter(t, "settings.foo>=7")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.Contains(t, output, "test2")
	})

	t.Run("less", func(t *testing.T) {
		output, err := runFilter(t, "settings.foo<42")
		require.NoError(t, err)
		assert.NotContains(t, output, "test1")
		assert.Contains(t, output, "test2")
	})

	t.Run("less_equal", func(t *testing.T) {
		output, err := runFilter(t, "settings.foo<=7")
		require.NoError(t, err)
		assert.NotContains(t, output, "test1")
		assert.Contains(t, output, "test2")
	})

	t.Run("float_field_vs_int_literal", func(t *testing.T) {
		output, err := runFilter(t, "settings.score<5")
		require.NoError(t, err)
		assert.Contains(t, output, "test2")
		assert.NotContains(t, output, "test1")
	})

	t.Run("float_field_greater_equal", func(t *testing.T) {
		output, err := runFilter(t, "settings.score>=10")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("invalid_literal_no_match_no_error", func(t *testing.T) {
		output, err := runFilter(t, "settings.foo>notanumber")
		require.NoError(t, err)
		assert.Empty(t, strings.TrimSpace(output))
	})

	t.Run("string_field_ordering_no_op", func(t *testing.T) {
		output, err := runFilter(t, "state>pending")
		require.NoError(t, err)
		assert.Empty(t, strings.TrimSpace(output))
	})

	t.Run("string_field_ordering_disallowed_even_with_differing_values", func(t *testing.T) {
		for _, filter := range []string{
			"settings.bar>hello",
			"settings.bar>=hello",
			"settings.bar<world",
			"settings.bar<=world",
		} {
			output, err := runFilter(t, filter)
			require.NoError(t, err)
			assert.Empty(t, strings.TrimSpace(output), "filter %q should not match any resource", filter)
		}

		output, err := runFilter(t, "settings.bar==hello")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("scalar_slice_indexed_equal", func(t *testing.T) {
		output, err := runFilter(t, "tags.0==prod")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("scalar_slice_wildcard_equal", func(t *testing.T) {
		output, err := runFilter(t, "tags.*==staging")
		require.NoError(t, err)
		assert.Contains(t, output, "test2")
		assert.NotContains(t, output, "test1")
	})

	t.Run("scalar_slice_wildcard_not_equal", func(t *testing.T) {
		output, err := runFilter(t, "tags.*!=prod")
		require.NoError(t, err)
		assert.Contains(t, output, "test2")
		assert.NotContains(t, output, "test1")
	})

	t.Run("bool_field_ordering_disallowed", func(t *testing.T) {
		for _, filter := range []string{
			"settings.flag>false",
			"settings.flag>=false",
			"settings.flag<true",
			"settings.flag<=true",
		} {
			output, err := runFilter(t, filter)
			require.NoError(t, err)
			assert.Empty(t, strings.TrimSpace(output), "filter %q should not match any resource", filter)
		}

		output, err := runFilter(t, "settings.flag==true")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("combined_with_equality", func(t *testing.T) {
		output, err := runFilter(t, "state==pending,settings.foo>7")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("relative_time_less_than", func(t *testing.T) {
		// test1 was created 3 days ago, test2 40 days ago.
		output, err := runFilter(t, "created<7d")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("relative_time_greater_than", func(t *testing.T) {
		output, err := runFilter(t, "created>7d")
		require.NoError(t, err)
		assert.Contains(t, output, "test2")
		assert.NotContains(t, output, "test1")
	})

	t.Run("relative_time_greater_equal_weeks", func(t *testing.T) {
		output, err := runFilter(t, "created>=5w")
		require.NoError(t, err)
		assert.Contains(t, output, "test2")
		assert.NotContains(t, output, "test1")
	})

	t.Run("relative_time_less_equal_compound_units", func(t *testing.T) {
		output, err := runFilter(t, "created<=3d1h")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("relative_time_invalid_no_match_no_error", func(t *testing.T) {
		output, err := runFilter(t, "created<notaduration")
		require.NoError(t, err)
		assert.Empty(t, strings.TrimSpace(output))
	})

	// cutoff sits between test1's creation time (3 days ago) and test2's (40
	// days ago), so it splits the two resources regardless of when the test
	// runs.
	cutoff := time.Now().Add(-10 * 24 * time.Hour).UTC()

	t.Run("absolute_time_rfc3339", func(t *testing.T) {
		output, err := runFilter(t, "created>"+cutoff.Format(time.RFC3339))
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("absolute_time_date_time_no_zone", func(t *testing.T) {
		output, err := runFilter(t, "created<"+cutoff.Format("2006-01-02T15:04:05"))
		require.NoError(t, err)
		assert.Contains(t, output, "test2")
		assert.NotContains(t, output, "test1")
	})

	t.Run("absolute_time_date_time_space", func(t *testing.T) {
		// The space needs quoting: the filter grammar splits unquoted
		// values on whitespace.
		output, err := runFilter(t, `created>="`+cutoff.Format("2006-01-02 15:04:05")+`"`)
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("absolute_time_date_only", func(t *testing.T) {
		output, err := runFilter(t, "created<="+cutoff.Format("2006-01-02"))
		require.NoError(t, err)
		assert.Contains(t, output, "test2")
		assert.NotContains(t, output, "test1")
	})

	t.Run("absolute_time_unix_seconds", func(t *testing.T) {
		output, err := runFilter(t, "created>"+strconv.FormatInt(cutoff.Unix(), 10))
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("absolute_time_invalid_no_match_no_error", func(t *testing.T) {
		output, err := runFilter(t, "created<notatimestamp")
		require.NoError(t, err)
		assert.Empty(t, strings.TrimSpace(output))
	})

	// test1 has usage 70/100 (70%), test2 has usage 30/100 (30%); see
	// setupTestEnv. MeterUsage orders by used/total ratio, same as sorting.
	t.Run("ratio_field_greater", func(t *testing.T) {
		output, err := runFilter(t, "usage>0.5")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("ratio_field_less", func(t *testing.T) {
		output, err := runFilter(t, "usage<0.5")
		require.NoError(t, err)
		assert.Contains(t, output, "test2")
		assert.NotContains(t, output, "test1")
	})

	t.Run("ratio_field_percent", func(t *testing.T) {
		output, err := runFilter(t, "usage>=50%")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("ratio_field_equal", func(t *testing.T) {
		output, err := runFilter(t, "usage==70%")
		require.NoError(t, err)
		assert.Contains(t, output, "test1")
		assert.NotContains(t, output, "test2")
	})

	t.Run("ratio_field_equal_rounds_to_display", func(t *testing.T) {
		// 11/16 is 68.75%, which String rounds to display as "69%"; the
		// filter should match on that displayed value, not the exact ratio.
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{
			ID:    "id-test3",
			Name:  "test3",
			Usage: types.MeterUsage[int]{Used: 11, Total: 16},
		})
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceListCmd[resourcet.TestResource]{
			Filter: []string{"usage==69%"},
			Output: Printer{Type: PrinterTypeQuiet},
		}
		err := cmd.Run(ctx, testStdio(&out), partition)
		require.NoError(t, err)
		assert.Contains(t, out.String(), "test3")
	})
}
