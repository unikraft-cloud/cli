// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package buildfs

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowsDirFS(t *testing.T) {
	fsys := WindowsDirFS(fstest.MapFS{
		".":            {Mode: fs.ModeDir | 0o777},
		"bin":          {Mode: fs.ModeDir | 0o777},
		"bin/app":      {Mode: 0o666, Data: []byte("app")},
		"bin/readonly": {Mode: 0o444, Data: []byte("ro")},
		"lib/link":     {Mode: fs.ModeSymlink | 0o777, Data: []byte(`..\bin\app`)},
	})

	modes := map[string]fs.FileMode{}
	require.NoError(t, fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		modes[path] = info.Mode()
		return nil
	}))

	assert.Equal(t, fs.ModeDir|0o755, modes["."], "the root is not world-writable")
	assert.Equal(t, fs.ModeDir|0o755, modes["bin"])
	assert.Equal(t, fs.FileMode(0o755), modes["bin/app"], "a file NTFS reports as 0666 can run")
	assert.Equal(t, fs.FileMode(0o555), modes["bin/readonly"], "a read-only file stays read-only")

	info, err := fs.Lstat(fsys, "lib/link")
	require.NoError(t, err)
	assert.Equal(t, fs.ModeSymlink|0o755, info.Mode())

	target, err := fs.ReadLink(fsys, "lib/link")
	require.NoError(t, err)
	assert.Equal(t, "../bin/app", target, "the guest reads forward slashes")

	data, err := fs.ReadFile(fsys, "bin/app")
	require.NoError(t, err)
	assert.Equal(t, "app", string(data), "contents pass through")
}
