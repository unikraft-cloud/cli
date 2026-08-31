// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/resource"
	resourcet "unikraft.com/cli/internal/resource/testing"
	"unikraft.com/cloud/sdk/platform/group"
)

func partialGet(missing string, remaining *int) func(context.Context, []string, func(context.Context, []string) ([]resource.Resource, error)) ([]resource.Resource, error) {
	return func(ctx context.Context, keys []string, next func(context.Context, []string) ([]resource.Resource, error)) ([]resource.Resource, error) {
		resources, err := next(ctx, keys)
		if err != nil {
			return resources, err
		}
		if remaining != nil {
			if *remaining <= 0 {
				return resources, nil
			}
			*remaining--
		}
		return resources, group.ErrRefNotFound{Refs: group.Refs{{Name: missing}}}
	}
}

func TestWaitPartialLookup(t *testing.T) {
	t.Run("never_succeeds_while_incomplete", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "id-test1", Name: "test1", State: "ready"})
		env.Hooks.Get = partialGet("missing", nil)

		ctx := resourcet.WithTestEnv(context.Background(), env)
		ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()

		var out bytes.Buffer
		cmd := &ResourceWaitCmd[resourcet.TestResource]{
			Targets:    []string{"test1", "missing"},
			Until:      []string{"state==ready"},
			Interval:   10 * time.Millisecond,
			FormatOpts: FormatOpts{Output: Printer{Type: PrinterTypeQuiet}},
		}
		err := cmd.Run(ctx, testStdio(&out), &resource.Sandbox{})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		var notFound group.ErrRefNotFound
		require.ErrorAs(t, err, &notFound)
		assert.Empty(t, out.String())
	})

	t.Run("succeeds_once_lookup_recovers", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "id-test1", Name: "test1", State: "ready"})
		flaky := 2
		env.Hooks.Get = partialGet("test1", &flaky)

		ctx := resourcet.WithTestEnv(context.Background(), env)
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		var out bytes.Buffer
		cmd := &ResourceWaitCmd[resourcet.TestResource]{
			Targets:    []string{"test1"},
			Until:      []string{"state==ready"},
			Interval:   10 * time.Millisecond,
			FormatOpts: FormatOpts{Output: Printer{Type: PrinterTypeQuiet}},
		}
		err := cmd.Run(ctx, testStdio(&out), &resource.Sandbox{})
		require.NoError(t, err)
		assert.Equal(t, 0, flaky)
		assert.Contains(t, out.String(), "test1")
	})

	t.Run("fails_when_nothing_is_found", func(t *testing.T) {
		env := resourcet.NewTestEnv()
		env.Hooks.Get = partialGet("missing", nil)

		ctx := resourcet.WithTestEnv(context.Background(), env)
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		cmd := &ResourceWaitCmd[resourcet.TestResource]{
			Targets:  []string{"missing"},
			Until:    []string{"state==ready"},
			Interval: 10 * time.Millisecond,
		}
		err := cmd.Run(ctx, testStdio(&bytes.Buffer{}), &resource.Sandbox{})
		require.Error(t, err)
		assert.NotErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestMutationReportsPartialLookup(t *testing.T) {
	newEnv := func() *resourcet.TestEnv {
		env := resourcet.NewTestEnv()
		env.Add(resourcet.TestResource{ID: "id-test1", Name: "test1", State: "ready"})
		env.Hooks.Get = partialGet("missing", nil)
		return env
	}

	t.Run("remove", func(t *testing.T) {
		env := newEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceRemoveCmd[resourcet.TestResource]{
			Targets:    []string{"test1"},
			FormatOpts: FormatOpts{Output: Printer{Type: PrinterTypeQuiet}},
		}
		err := cmd.Run(ctx, testStdio(&out), &resource.Sandbox{})
		var notFound group.ErrRefNotFound
		require.ErrorAs(t, err, &notFound)
		assert.Contains(t, out.String(), "test1")
	})

	t.Run("remove_bulk", func(t *testing.T) {
		env := newEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceBulkRemoveCmd[resourcet.TestResource]{
			Targets:    []string{"test1"},
			FormatOpts: FormatOpts{Output: Printer{Type: PrinterTypeQuiet}},
		}
		err := cmd.Run(ctx, testStdio(&out), &resource.Sandbox{})
		var notFound group.ErrRefNotFound
		require.ErrorAs(t, err, &notFound)
		assert.Contains(t, out.String(), "test1")
	})

	t.Run("edit", func(t *testing.T) {
		env := newEnv()
		ctx := resourcet.WithTestEnv(context.Background(), env)

		var out bytes.Buffer
		cmd := &ResourceEditCmd[resourcet.TestResource]{
			Target:     "test1",
			SetArgs:    SetArgs{Set: []map[string]string{{"settings.foo": "999"}}},
			FormatOpts: FormatOpts{Output: Printer{Type: PrinterTypeQuiet}},
		}
		err := cmd.Run(ctx, testStdio(&out), &resource.Sandbox{})
		var notFound group.ErrRefNotFound
		require.ErrorAs(t, err, &notFound)
		assert.Contains(t, out.String(), "test1")
	})
}

func TestCreateReportsPartialLookup(t *testing.T) {
	env := resourcet.NewTestEnv()
	env.Hooks.Create = func(ctx context.Context, fields []resource.Field, next func(context.Context, []resource.Field) ([]resource.Resource, error)) ([]resource.Resource, error) {
		resources, err := next(ctx, fields)
		if err != nil {
			return resources, err
		}
		return resources, group.ErrRefNotFound{Refs: group.Refs{{Name: "missing"}}}
	}
	ctx := resourcet.WithTestEnv(context.Background(), env)

	var out bytes.Buffer
	cmd := &ResourceCreateCmd[resourcet.TestResource]{
		SetArgs:    SetArgs{Set: []map[string]string{{"name": "test1"}}},
		FormatOpts: FormatOpts{Output: Printer{Type: PrinterTypeQuiet}},
	}
	err := cmd.Run(ctx, testStdio(&out), &resource.Sandbox{})
	var notFound group.ErrRefNotFound
	require.ErrorAs(t, err, &notFound)
	assert.Contains(t, out.String(), "test1")
}

func TestMutationFailureSurvivesPartialLookup(t *testing.T) {
	env := resourcet.NewTestEnv()
	env.Add(resourcet.TestResource{ID: "id-test1", Name: "test1", State: "ready"})
	env.Hooks.Get = partialGet("missing", nil)
	env.Hooks.Delete = func(context.Context, []string, func(context.Context, []string) error) error {
		return group.ErrRefNotFound{Refs: group.Refs{{Name: "test1"}}}
	}
	ctx := resourcet.WithTestEnv(context.Background(), env)

	cmd := &ResourceRemoveCmd[resourcet.TestResource]{
		Targets:    []string{"test1"},
		FormatOpts: FormatOpts{Output: Printer{Type: PrinterTypeQuiet}},
	}
	require.Error(t, cmd.Run(ctx, testStdio(&bytes.Buffer{}), &resource.Sandbox{}))
}
