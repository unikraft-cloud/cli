// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package model

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPartitionFromEnv(t *testing.T) {
	const env = "TEST_PARTITION"
	path := filepath.Join(t.TempDir(), "partition.json")

	t.Run("no env", func(t *testing.T) {
		t.Setenv(env, path)
		p, err := LoadPartitionFromEnv("")
		require.NoError(t, err)
		assert.Nil(t, p)
	})

	t.Run("unset", func(t *testing.T) {
		p, err := LoadPartitionFromEnv(env)
		require.NoError(t, err)
		assert.Nil(t, p)
	})

	t.Run("set", func(t *testing.T) {
		t.Setenv(env, path)
		p, err := LoadPartitionFromEnv(env)
		require.NoError(t, err)
		require.NotNil(t, p)
		assert.Equal(t, path, p.Path)
	})
}
