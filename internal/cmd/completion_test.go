// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
)

func TestWithCompletionConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("COMP_LINE", "unikraft --config="+path+" instance get --profile bar ")

	ctx, err := withCompletionConfig(t.Context())
	require.NoError(t, err)
	cfg := config.G(ctx)
	require.NotNil(t, cfg)
	assert.Equal(t, path, cfg.Path)
	assert.Equal(t, "bar", cfg.CurrentProfileName())
}

func TestWithCompletionConfigNotCompleting(t *testing.T) {
	t.Setenv("COMP_LINE", "")

	parent := t.Context()
	ctx, err := withCompletionConfig(parent)
	require.NoError(t, err)
	assert.Equal(t, parent, ctx)
}
