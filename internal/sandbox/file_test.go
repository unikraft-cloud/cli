// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStatLocalFile pins what a local source is accepted as: a symlink is
// followed to whatever it points at, a hard link is an ordinary file, and a
// symlink that points nowhere names both itself and its target.
func TestStatLocalFile(t *testing.T) {
	dir := t.TempDir()

	regular := filepath.Join(dir, "regular.txt")
	require.NoError(t, os.WriteFile(regular, []byte("contents\n"), 0o644))

	subdir := filepath.Join(dir, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0o755))

	big := filepath.Join(dir, "big.bin")
	require.NoError(t, os.WriteFile(big, nil, 0o644))
	require.NoError(t, os.Truncate(big, maxFileSize+1))

	hard := filepath.Join(dir, "hard.txt")
	require.NoError(t, os.Link(regular, hard))

	symlink := func(name, target string) string {
		t.Helper()
		link := filepath.Join(dir, name)
		require.NoError(t, os.Symlink(target, link))
		return link
	}

	for _, tt := range []struct {
		name  string
		local string
		wants []string
	}{
		{name: "regular", local: regular},
		{name: "symlink-to-file", local: symlink("to-file", regular)},
		{name: "hard-link", local: hard},
		{name: "directory", local: subdir, wants: []string{"is a directory", "written"}},
		{name: "symlink-to-directory", local: symlink("to-dir", subdir), wants: []string{"is a directory"}},
		{name: "dangling-symlink", local: symlink("dangling", filepath.Join(dir, "missing.txt")), wants: []string{"dangling", "missing.txt", "does not exist"}},
		{name: "over-the-limit", local: big, wants: []string{"over the", "limit"}},
		{name: "symlink-over-the-limit", local: symlink("to-big", big), wants: []string{"over the", "limit"}},
		{name: "missing", local: filepath.Join(dir, "nope.txt"), wants: []string{"reading local file", "nope.txt"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := StatLocalFile(tt.local, "written")
			if len(tt.wants) == 0 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, want := range tt.wants {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}
