// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestSanity(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configName string
	}{
		{name: "busybox-sanity-staging", configName: "config.yaml"},
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
}
