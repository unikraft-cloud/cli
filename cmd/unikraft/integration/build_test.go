// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"fmt"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestBuild(t *testing.T) {
	// NOTE: only erofs is supported for ROM automounting by the Unikraft
	// kernel currently. CPIO ROMs are not automounted (the kernel hardcodes
	// erofs as the fs type for ROM mounts), so the CPIO variant omits
	// the at= mount option.
	for _, romFormat := range []string{"erofs", "cpio"} {
		t.Run("rom-"+romFormat, func(t *testing.T) {
			r := runner(t, true, []string{staging, stable})
			image := integ.Busybox.Build(t, r)
			romImagePrefix := r.Config.Profile.Organization + "/rom-" + romFormat + "-e2e"

			imageTag := uniq()
			instName := uniq()
			romImage := romImagePrefix + ":" + imageTag

			// Only erofs ROMs support kernel automounting via at=.
			// For CPIO, we extract manually from the block device.
			romFlag := "image=" + romImage + ",name=myrom"
			var args string
			if romFormat == "erofs" {
				romFlag += ",at=/rom"
				args = "cat /rom/hello.txt"
			} else {
				args = "sh -c 'cd /tmp && cpio -id < /dev/ukp_rom_myrom && cat hello.txt'"
			}

			// ROM-only image context: just a directory with a text file.
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateDir("rom", 0o755),
				fstest.CreateDir("rom/myrom", 0o755),
				fstest.CreateFile("rom/myrom/hello.txt", []byte("Hello from ROM!\n"), 0o644),
				fstest.CreateFile("rom/Kraftfile", fmt.Appendf(nil, `
spec: v0.7
name: rom-%s-e2e
roms:
  - source: ./myrom
    format: %s
`, romFormat, romFormat), 0o644),
			).Apply(dir))

			r.Run(t, []string{"unikraft", "build", "rom", "--arch", "x86_64", "--output", romImage}, integ.WithWorkDir(dir))
			r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", image, "--rom", romFlag, "--args", args})
			r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})

			out := r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
			assert.Regexp(t, `Hello from ROM!`, out)

			r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		})
	}

	t.Run("rom-dir", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		image := integ.Busybox.Build(t, r)

		instName := uniq()

		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("romdata", 0o755),
			fstest.CreateFile("romdata/hello.txt", []byte("Hello from ROM!\n"), 0o644),
		).Apply(dir))

		r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", image, "--rom", "dir=romdata,at=/rom", "--args", "cat /rom/hello.txt"}, integ.WithWorkDir(dir))
		r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})

		out := r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
		assert.Regexp(t, `Hello from ROM!`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("busybox", func(t *testing.T) {
		variants := []string{"registry", "direct-push"}
		for _, format := range []string{"cpio", "erofs"} {
			t.Run(format, func(t *testing.T) {
				for _, name := range variants {
					t.Run(name, func(t *testing.T) {
						r := runner(t, true, []string{staging, stable})
						var imagePrefix string
						switch name {
						case "registry":
							imagePrefix = r.Config.Profile.Organization + "/busybox-e2e"
						case "direct-push":
							imagePrefix = r.Config.Metro.Index().Host + "/" + r.Config.Profile.Organization + "/busybox-e2e"
						}
						imageTag := uniq()
						instName := uniq()
						image := imagePrefix + ":" + imageTag

						dir := t.TempDir()
						require.NoError(t, fstest.Apply(
							fstest.CreateFile("Dockerfile", []byte(`
FROM busybox:latest
RUN echo "unikraft-e2e" > /etc/unikraft-e2e
COPY <<EOF /entrypoint.sh
#!/bin/sh
echo "== BEGIN /etc/unikraft-e2e =="
cat /etc/unikraft-e2e
echo "== END /etc/unikraft-e2e =="
echo "== BEGIN ls /etc/unikraft-e2e =="
ls /etc/unikraft-e2e
echo "== END ls /etc/unikraft-e2e =="
echo "== BEGIN status =="
echo UNIKRAFT_E2E_OK
echo "== END status =="
EOF
RUN chmod +x /entrypoint.sh
`), 0o644),
							fstest.CreateFile("Kraftfile", fmt.Appendf(nil, `
spec: v0.7
name: busybox-e2e
runtime: base-compat:latest
rootfs:
  format: %s
  source: ./Dockerfile
cmd: ["sh", "/entrypoint.sh"]
`, format), 0o644),
						).Apply(dir))

						r.Run(t, []string{"unikraft", "build", ".", "--output", image}, integ.WithWorkDir(dir))

						out := r.Run(t, []string{"unikraft", "image", "inspect", image})
						assert.Regexp(t, `busybox-e2e`, out)

						r.Run(t, []string{"unikraft", "image", "ls", image, "-okv"})
						r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", image})
						r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})

						out = r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
						assert.Regexp(t, `UNIKRAFT_E2E_OK`, out)
						assert.Regexp(t, `== BEGIN /etc/unikraft-e2e ==`, out)
						assert.Regexp(t, `unikraft-e2e`, out)
						assert.Regexp(t, `== END /etc/unikraft-e2e ==`, out)
						assert.Regexp(t, `== BEGIN ls /etc/unikraft-e2e ==`, out)
						assert.Regexp(t, `/etc/unikraft-e2e`, out)
						assert.Regexp(t, `== END ls /etc/unikraft-e2e ==`, out)

						r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})

						r.Run(t, []string{"unikraft", "image", "delete", image})
						r.Run(t, []string{"unikraft", "image", "inspect", image}, integ.ExpectFail())
						r.Run(t, []string{"unikraft", "image", "ls", image}, integ.ExpectFail())
					})
				}
			})
		}
	})

	// A project with only a Dockerfile builds without a Kraftfile. The
	// architecture then comes from --arch. Both a directory and a direct
	// Dockerfile path are accepted as input.
	t.Run("dockerfile-only", func(t *testing.T) {
		for _, input := range []string{".", "Dockerfile"} {
			t.Run(input, func(t *testing.T) {
				r := runner(t, true, []string{staging, stable})
				imageTag := uniq()
				instName := uniq()
				image := r.Config.Profile.Organization + "/dockerfile-e2e:" + imageTag

				dir := t.TempDir()
				require.NoError(t, fstest.Apply(
					fstest.CreateFile("Dockerfile", []byte(`
FROM busybox:latest
RUN echo "unikraft-e2e" > /etc/unikraft-e2e
CMD ["sh", "-c", "cat /etc/unikraft-e2e && echo UNIKRAFT_E2E_OK"]
`), 0o644),
				).Apply(dir))

				// Without --arch there is nothing to say which platform to build.
				r.Run(t, []string{"unikraft", "build", input, "--output", image}, integ.WithWorkDir(dir), integ.ExpectFail())

				r.Run(t, []string{"unikraft", "build", input, "--arch", "x86_64", "--output", image}, integ.WithWorkDir(dir))

				out := r.Run(t, []string{"unikraft", "image", "inspect", image})
				assert.Regexp(t, `dockerfile-e2e`, out)

				r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", image})
				r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})

				out = r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
				assert.Regexp(t, `unikraft-e2e`, out)
				assert.Regexp(t, `UNIKRAFT_E2E_OK`, out)

				r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
				r.Run(t, []string{"unikraft", "image", "delete", image})
			})
		}
	})

	// A Kraftfile given directly is parsed, and its relative rootfs source
	// resolves against the directory of the Kraftfile, not the work directory.
	t.Run("kraftfile-path", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		imageTag := uniq()
		instName := uniq()
		image := r.Config.Profile.Organization + "/kraftfile-path-e2e:" + imageTag

		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			// A Dockerfile in the work directory that the build must not use.
			fstest.CreateFile("Dockerfile", []byte(`
FROM busybox:latest
RUN echo "wrong-context" > /etc/unikraft-e2e
`), 0o644),
			fstest.CreateDir("proj", 0o755),
			fstest.CreateFile("proj/Dockerfile", []byte(`
FROM busybox:latest
RUN echo "right-context" > /etc/unikraft-e2e
CMD ["sh", "-c", "echo DOCKERFILE_CMD_USED"]
`), 0o644),
			fstest.CreateFile("proj/Kraftfile", []byte(`
spec: v0.7
name: kraftfile-path-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["sh", "-c", "cat /etc/unikraft-e2e && echo UNIKRAFT_E2E_OK"]
`), 0o644),
		).Apply(dir))

		// The Kraftfile declares a runtime, thus --arch is not necessary.
		r.Run(t, []string{"unikraft", "build", "proj/Kraftfile", "--output", image}, integ.WithWorkDir(dir))

		out := r.Run(t, []string{"unikraft", "image", "inspect", image})
		assert.Regexp(t, `kraftfile-path-e2e`, out)

		r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", image})
		r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})

		out = r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
		assert.Regexp(t, `right-context`, out)
		assert.Regexp(t, `UNIKRAFT_E2E_OK`, out)
		// The rootfs comes from proj/Dockerfile, and the cmd from the Kraftfile.
		assert.NotRegexp(t, `wrong-context`, out)
		assert.NotRegexp(t, `DOCKERFILE_CMD_USED`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "image", "delete", image})
	})

	// A directory that has a Kraftfile and a Dockerfile builds from the
	// Kraftfile, which gives the runtime and the cmd of the image.
	t.Run("kraftfile-precedence", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		imageTag := uniq()
		instName := uniq()
		image := r.Config.Profile.Organization + "/kraftfile-precedence-e2e:" + imageTag

		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateFile("Dockerfile", []byte(`
FROM busybox:latest
RUN echo "unikraft-e2e" > /etc/unikraft-e2e
CMD ["sh", "-c", "echo DOCKERFILE_CMD_USED"]
`), 0o644),
			fstest.CreateFile("Kraftfile", []byte(`
spec: v0.7
name: kraftfile-precedence-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["sh", "-c", "cat /etc/unikraft-e2e && echo KRAFTFILE_CMD_USED"]
`), 0o644),
		).Apply(dir))

		// A build from the Dockerfile alone needs --arch. A build that
		// succeeds without it shows that the Kraftfile has precedence.
		r.Run(t, []string{"unikraft", "build", ".", "--output", image}, integ.WithWorkDir(dir))

		out := r.Run(t, []string{"unikraft", "image", "inspect", image})
		assert.Regexp(t, `kraftfile-precedence-e2e`, out)

		r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", image})
		r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})

		out = r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
		assert.Regexp(t, `unikraft-e2e`, out)
		assert.Regexp(t, `KRAFTFILE_CMD_USED`, out)
		assert.NotRegexp(t, `DOCKERFILE_CMD_USED`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "image", "delete", image})
	})

	t.Run("shared-run", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		image := integ.Busybox.Build(t, r)

		instName := uniq()

		r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", image, "--args", "echo UNIKRAFT_E2E_OK"})
		r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})

		out := r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
		assert.Regexp(t, `UNIKRAFT_E2E_OK`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})
}
