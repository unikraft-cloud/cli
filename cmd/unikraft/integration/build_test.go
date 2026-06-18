// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestBuild(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configName string
	}{
		{name: "busybox-sanity-stable", configName: "config-stable.yaml"},
		{name: "busybox-sanity-prod", configName: "config-prod.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runner(t, true, tc.configName)
			image := r.Config.Profile.Organization + "/busybox-sanity-e2e:" + uniq()
			instance := "test-" + uniq()

			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("Dockerfile", []byte(`
FROM busybox:latest
COPY <<EOF /entrypoint.sh
#!/bin/sh
echo UNIKRAFT_E2E_OK
EOF
RUN chmod +x /entrypoint.sh
`), 0o644),
				fstest.CreateFile("Kraftfile", []byte(`
spec: v0.7
name: busybox-sanity-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["sh", "/entrypoint.sh"]
`), 0o644),
			).Apply(dir))

			r.Run(t, []string{"unikraft", "build", ".", "--output", image}, integ.WithWorkDir(dir))
			if tc.name == "busybox-sanity-prod" {
				// Wait for image propagation
				// NOTE(craciunoiuc): To be removed when nodes are updated
				time.Sleep(5 * time.Second)
			}

			r.Run(t, []string{"unikraft", "run", "--name", instance, "--metro", r.Config.MetroName, "--output", "quiet", "--image", image}, integ.WithWorkDir(dir))
			r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==stopped", "--timeout", "10s", instance})

			out := r.Run(t, []string{"unikraft", "instance", "logs", instance})
			assert.Regexp(t, `UNIKRAFT_E2E_OK`, out)

			r.Run(t, []string{"unikraft", "instance", "rm", instance})
			r.Run(t, []string{"unikraft", "image", "rm", image})
		})
	}

	// NOTE: only erofs is supported for ROM automounting by the Unikraft
	// kernel currently. CPIO ROMs are not automounted (the kernel hardcodes
	// erofs as the fs type for ROM mounts), so the CPIO variant omits
	// the at= mount option.
	for _, romFormat := range []string{"erofs", "cpio"} {
		t.Run("rom-"+romFormat, func(t *testing.T) {
			r := runner(t, true)
			romImagePrefix := r.Config.Profile.Organization + "/rom-" + romFormat + "-e2e"
			baseImagePrefix := r.Config.Profile.Organization + "/busybox-rom-" + romFormat + "-e2e"

			// Only erofs ROMs support kernel automounting via at=.
			// For CPIO, we extract manually from the block device.
			entryCmd := []string{"sh", "-c", "cd /tmp && cpio -id < /dev/ukp_rom_myrom && cat hello.txt"}
			if romFormat == "erofs" {
				entryCmd = []string{"cat", "/rom/hello.txt"}
			}

			imageTag := uniq()
			instName := uniq()
			romImage := romImagePrefix + ":" + imageTag
			baseImage := baseImagePrefix + ":" + imageTag
			romFlag := "image=" + romImage + ",name=myrom"
			if romFormat == "erofs" {
				romFlag += ",at=/rom"
			}

			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateDir("base", 0o755),
				fstest.CreateFile("base/Dockerfile", []byte(`FROM busybox:latest`), 0o644),
				fstest.CreateFile("base/Kraftfile", fmt.Appendf(nil, `
spec: v0.7
name: busybox-rom-%s-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["%s"]
`, romFormat, strings.Join(entryCmd, `", "`)), 0o644),
				// ROM-only image context: just a directory with a text file.
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

			r.Run(t, []string{"unikraft", "build", "base", "--output", baseImage}, integ.WithWorkDir(dir))
			r.Run(t, []string{"unikraft", "build", "rom", "--output", romImage}, integ.WithWorkDir(dir))
			r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", baseImage, "--rom", romFlag})
			r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})

			out := r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
			assert.Regexp(t, `Hello from ROM!`, out)

			r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		})
	}

	t.Run("rom-dir", func(t *testing.T) {
		r := runner(t, true)
		baseImagePrefix := r.Config.Profile.Organization + "/busybox-romdir-e2e"

		imageTag := uniq()
		instName := uniq()
		baseImage := baseImagePrefix + ":" + imageTag

		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("base", 0o755),
			fstest.CreateFile("base/Dockerfile", []byte(`FROM busybox:latest`), 0o644),
			fstest.CreateFile("base/Kraftfile", []byte(`
spec: v0.7
name: busybox-romdir-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/rom/hello.txt"]
`), 0o644),
			fstest.CreateDir("romdata", 0o755),
			fstest.CreateFile("romdata/hello.txt", []byte("Hello from ROM!\n"), 0o644),
		).Apply(dir))

		r.Run(t, []string{"unikraft", "build", "base", "--output", baseImage}, integ.WithWorkDir(dir))
		r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", baseImage, "--rom", "dir=romdata,at=/rom"}, integ.WithWorkDir(dir))
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
						r := runner(t, true)
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

	t.Run("busybox-custom-dockerfile", func(t *testing.T) {
		r := runner(t, true)
		imagePrefix := r.Config.Profile.Organization + "/busybox-custom-df-e2e"
		imageTag := uniq()
		instName := uniq()
		image := imagePrefix + ":" + imageTag

		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateFile("App.dockerfile", []byte(`
FROM busybox:latest
COPY <<EOF /entrypoint.sh
#!/bin/sh
echo UNIKRAFT_E2E_OK
EOF
RUN chmod +x /entrypoint.sh
`), 0o644),
			fstest.CreateFile("Kraftfile", []byte(`
spec: v0.7
name: busybox-custom-df-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source:
    path: .
    dockerfile: App.dockerfile
cmd: ["sh", "/entrypoint.sh"]
`), 0o644),
		).Apply(dir))

		r.Run(t, []string{"unikraft", "build", ".", "--output", image}, integ.WithWorkDir(dir))
		r.Run(t, []string{"unikraft", "run", "--name", "test-" + instName, "--metro", r.Config.MetroName, "--output", "quiet", "--image", image})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==stopped", "--timeout", "10s", "test-" + instName})

		out := r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
		assert.Regexp(t, `UNIKRAFT_E2E_OK`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "image", "delete", image})
	})
}
