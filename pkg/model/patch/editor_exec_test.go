// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package patch

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitEditor(t *testing.T) {
	args, err := splitEditor("unikraft-missing-editor --wait")
	require.NoError(t, err)
	assert.Equal(t, []string{"unikraft-missing-editor", "--wait"}, args, "flags follow the program")

	name := "my editor"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	whole := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(whole, nil, 0o755))
	args, err = splitEditor(whole)
	require.NoError(t, err)
	assert.Equal(t, []string{whole}, args, "a program with a space in its path is not split")
}
