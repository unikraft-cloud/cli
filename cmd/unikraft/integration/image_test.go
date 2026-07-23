// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestImages(t *testing.T) {
	t.Run("inspect", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		out := r.Run(t, []string{"unikraft", "image", "inspect", "nginx:latest"})
		assert.Regexp(t, `ref:\s+nginx`, out)
		assert.Regexp(t, `config:`, out)
		assert.Regexp(t, `kernel:`, out)
		assert.Regexp(t, `kernel.dbg:`, out)
	})

	t.Run("copy-inspect-delete", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		imageTag := uniq()
		imageName := r.Config.Profile.Organization + "/nginx-copy:" + imageTag
		imageFull := fmt.Sprintf("%s/%s", r.Config.Metro.Index().Host, imageName)

		r.Run(t, []string{"unikraft", "image", "copy", "nginx:latest", imageFull})

		out := r.Run(t, []string{"unikraft", "image", "inspect", imageFull})
		assert.Regexp(t, `ref:\s+.*`+imageName, out)
		assert.Regexp(t, `config:`, out)
		assert.Regexp(t, `kernel:`, out)
		assert.Regexp(t, `kernel.dbg:`, out)
		r.Run(t, []string{"unikraft", "image", "delete", imageFull})
	})

	// unpack exercises image unpack across both supported rootfs formats
	// (CPIO and EROFS). A known image is built with deterministic rootfs
	// content, then unpacked to a directory whose tree is asserted on disk.
	t.Run("unpack", func(t *testing.T) {
		for _, format := range []string{"cpio", "erofs"} {
			t.Run(format, func(t *testing.T) {
				r := runner(t, true, []string{staging, stable})

				imageTag := uniq()
				imagePrefix := r.Config.Profile.Organization + "/unpack-" + format + "-e2e"
				image := imagePrefix + ":" + imageTag

				// Build an image whose rootfs carries a known directory tree,
				// a regular file, and a symlink.
				dir := t.TempDir()
				require.NoError(t, fstest.Apply(
					fstest.CreateFile("Dockerfile", []byte(`FROM busybox:latest
RUN mkdir -p /app/sub && echo "unpack-e2e" > /app/hello.txt \
	&& echo "nested" > /app/sub/nested.txt \
	&& ln -s hello.txt /app/link.txt
`), 0o644),
					fstest.CreateFile("Kraftfile", []byte(fmt.Sprintf(`
spec: v0.7
name: unpack-%s-e2e
runtime: base-compat:latest
rootfs:
  format: %s
  source: ./Dockerfile
cmd: ["sh"]
`, format, format)), 0o644),
				).Apply(dir))

				r.Run(t, []string{"unikraft", "build", ".", "--output", image}, integ.WithWorkDir(dir))

				// Unpack the freshly built image into a temp directory.
				outDir := t.TempDir()
				r.Run(t, []string{"unikraft", "image", "unpack", image, outDir})

				// Regular file content round-trips intact.
				got, err := os.ReadFile(filepath.Join(outDir, "app/hello.txt"))
				require.NoError(t, err)
				assert.Equal(t, "unpack-e2e\n", string(got))

				gotNested, err := os.ReadFile(filepath.Join(outDir, "app/sub/nested.txt"))
				require.NoError(t, err)
				assert.Equal(t, "nested\n", string(gotNested))

				// Nested directory exists.
				fi, err := os.Stat(filepath.Join(outDir, "app/sub"))
				require.NoError(t, err)
				assert.True(t, fi.IsDir())

				// Symlink target preserved.
				gotLink, err := os.Readlink(filepath.Join(outDir, "app/link.txt"))
				require.NoError(t, err)
				assert.Equal(t, "hello.txt", gotLink)

				r.Run(t, []string{"unikraft", "image", "delete", image})
			})
		}
	})
}
