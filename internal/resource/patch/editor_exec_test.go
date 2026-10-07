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

	if runtime.GOOS == "windows" {
		args, err = splitEditor(`"C:\Program Files\Editor\editor.exe" -multiInst`)
		require.NoError(t, err)
		assert.Equal(t, []string{`C:\Program Files\Editor\editor.exe`, "-multiInst"}, args,
			"backslashes in a quoted Windows path are kept")
	}
}

func TestInterpretCommand(t *testing.T) {
	output, err := interpretCommand(t.Context(), `while read -r line; do echo "$line # edited"; done`, []byte("name: app\n"))
	require.NoError(t, err)
	assert.Equal(t, "name: app # edited\n", string(output), "the command edits stdin onto stdout")

	_, err = interpretCommand(t.Context(), "exit 3", nil)
	assert.Error(t, err, "a command that fails is an error")
}
