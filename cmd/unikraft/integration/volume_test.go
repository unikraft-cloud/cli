// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestVolumes(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()

		out := r.Run(t, []string{"unikraft", "volume", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))

		out = r.Run(t, []string{"unikraft", "volume", "create", "--set", "name=test-" + volName, "--set", "size=10", "--set", "metro=" + r.MetroName})
		assert.Regexp(t, `state:\s+(available|initializing)`, out)
		assert.Regexp(t, `size:\s+10`, out)
		assert.Regexp(t, `filesystem:`, out)

		out = r.Run(t, []string{"unikraft", "volume", "list"})
		assert.Regexp(t, `test-.*available`, out)

		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.Regexp(t, `state:\s+(available|initializing)`, out)
		assert.Regexp(t, `size:\s+10`, out)

		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		out = r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
		assert.Regexp(t, `test-`, out)
	})

	t.Run("edit", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()

		r.Run(t, []string{"unikraft", "volume", "create", "--output", "quiet", "--set", "name=test-" + volName, "--set", "size=10", "--set", "metro=" + r.MetroName})
		r.Run(t, []string{"unikraft", "volume", "edit", "test-" + volName, "--output", "quiet", "--set", "size=20"})

		out := r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.Regexp(t, `size:\s+20`, out)

		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("clone", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()
		cloneName := uniq()

		r.Run(t, []string{"unikraft", "volume", "create", "--output", "quiet", "--set", "name=test-" + volName, "--set", "size=10", "--set", "metro=" + r.MetroName})
		r.Run(t, []string{"unikraft", "volume", "clone", "test-" + volName, "--output", "quiet", "--set", "name=test-" + cloneName})

		out := r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName, "test-" + cloneName})
		assert.Regexp(t, `state:\s+available`, out)
		assert.Regexp(t, `size:\s+10`, out)

		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + cloneName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + cloneName})
	})

	t.Run("access-mode-rwo", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()

		out := r.Run(t, []string{
			"unikraft", "volume", "create",
			"--set", "name=test-" + volName,
			"--set", "size=10",
			"--set", "metro=" + r.MetroName,
			"--access-mode", "rwo",
		})
		assert.Regexp(t, `state:\s+(available|initializing)`, out)
		assert.Regexp(t, `access-mode:\s+rwo`, out)

		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.Regexp(t, `access-mode:\s+rwo`, out)

		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("access-mode-rox", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()
		instName1 := uniq()
		instName2 := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=10",
			"--set", "metro=" + r.MetroName,
			"--access-mode", "rox",
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName1,
			"--set", "metro=" + r.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/mnt:ro",
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName2,
			"--set", "metro=" + r.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/mnt:ro",
		})

		out := r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.Regexp(t, `access-mode:\s+rox`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName1})
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName2})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("attach-detach", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=10",
			"--set", "metro=" + r.MetroName,
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.MetroName,
			"--set", "image=nginx:latest",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		// attach shows instance diff
		out := r.Run(t, []string{
			"unikraft", "volume", "attach",
			"test-" + volName,
			"--to", "test-" + instName,
			"--at", "/data",
		})
		assert.Regexp(t, `test-`+volName, out)

		// verify via volume inspect
		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.Regexp(t, `test-`+instName, out)

		// detach shows instance diff
		out = r.Run(t, []string{
			"unikraft", "volume", "detach",
			"test-" + volName,
			"--from", "test-" + instName,
		})
		assert.Regexp(t, `test-`+instName, out)

		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.NotRegexp(t, `test-`+instName, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("attach-readonly", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--name", "test-" + volName,
			"--size", "10",
			"--metro", r.MetroName,
		})
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--name", "test-" + instName,
			"--metro", r.MetroName,
			"--image", "nginx:latest",
			"--memory", "128",
			"--vcpus", "1",
		})

		r.Run(t, []string{
			"unikraft", "volume", "attach", "test-" + volName,
			"--to", "test-" + instName,
			"--at", "/data",
			"--readonly",
		})
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName, "--output", "json"})
		assert.Regexp(t, `"at":\s*"/data"`, out)
		assert.Regexp(t, `"readonly":\s*true`, out)

		r.Run(t, []string{
			"unikraft", "volume", "detach", "test-" + volName,
			"--from", "test-" + instName,
		})
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("import", func(t *testing.T) {
		t.Run("missing-source", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable})
			out := r.Run(t, []string{"unikraft", "volume", "import", "my-volume"}, integ.ExpectFail())
			assert.Regexp(t, `source path is required`, out)
		})

		t.Run("invalid-port", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable})
			out := r.Run(t, []string{"unikraft", "volume", "import", "my-volume", "--source", ".", "--port", "80"}, integ.ExpectFail())
			assert.Regexp(t, `port must be between`, out)
		})

		t.Run("invalid-port-high", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable})
			out := r.Run(t, []string{"unikraft", "volume", "import", "my-volume", "--source", ".", "--port", "99999"}, integ.ExpectFail())
			assert.Regexp(t, `port must be between`, out)
		})

		t.Run("dir", func(t *testing.T) {
			// NOTE: re-enable on stable once mounting volumes works correctly on the
			// kernel when used together with the latest platform.
			// r := runner(t, true, []string{staging, stable})
			r := runner(t, true, []string{staging})

			volName := uniq()
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("hello.txt", []byte("hello from volume import\n"), 0o644),
			).Apply(dir))

			r.Run(t, []string{"unikraft", "volume", "create", "--output", "quiet", "--set", "name=test-" + volName, "--set", "size=10", "--set", "metro=" + r.MetroName})

			out := r.Run(t, []string{"unikraft", "volume", "import", "test-" + volName, "--source", "."}, integ.WithWorkDir(dir))
			assert.Regexp(t, `import complete`, out)

			out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
			assert.Regexp(t, `state:\s+available`, out)

			r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
			r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
		})

		t.Run("serve", func(t *testing.T) {
			// NOTE: re-enable on stable once mounting volumes works correctly on the
			// kernel when used together with the latest platform.
			// r := runner(t, true, []string{staging, stable})
			r := runner(t, true, []string{staging})

			volName := uniq()
			instName := uniq()
			domainName := uniq()
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("index.html", []byte("<html><body>hello from volume import</body></html>\n"), 0o644),
			).Apply(dir))

			r.Run(t, []string{
				"unikraft", "volume", "create",
				"--output", "quiet",
				"--set", "name=test-" + volName,
				"--set", "size=50",
				"--set", "metro=" + r.MetroName,
			})

			out := r.Run(t, []string{"unikraft", "volume", "import", "test-" + volName, "--source", "."}, integ.WithWorkDir(dir))
			assert.Regexp(t, `import complete`, out)

			r.Run(t, []string{
				"unikraft", "instance", "create",
				"--set", "name=test-" + instName,
				"--set", "metro=" + r.MetroName,
				"--set", "image=nginx:latest",
				"--set", "autostart=true",
				"--set", "resources.memory=256",
				"--set", "resources.vcpus=1",
				"--set", "volumes=test-" + volName + ":/wwwroot",
				"--set", "service.services=443:8080/tls+http",
				"--set", "service.domains=name=" + domainName,
			})

			out = r.Run(t, []string{
				"unikraft", "instance", "inspect", "test-" + instName,
				"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
			})
			fqdn := strings.TrimSpace(out)

			r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==running", "test-" + instName})

			body := integ.HTTPGet(t, "https://"+fqdn)
			assert.Contains(t, body, "hello from volume import")

			r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
			r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
			r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
		})
	})

	t.Run("tags", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()

		// Create volume with tags.
		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=10",
			"--set", "metro=" + r.MetroName,
			"--tag", "env-prod",
			"--tag", "team-core",
		})

		// Verify tags appear in inspect output.
		out := r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.Regexp(t, `tags:.*env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)

		// Filter by tag.
		out = r.Run(t, []string{"unikraft", "volume", "list", "--filter", "tags.*==env-prod"})
		assert.Contains(t, out, "test-"+volName)

		out = r.Run(t, []string{"unikraft", "volume", "list", "--filter", "tags.*==no-match"})
		assert.NotContains(t, out, "test-"+volName)

		// Edit: set (replace all tags).
		r.Run(t, []string{
			"unikraft", "volume", "edit", "test-" + volName,
			"--output", "quiet",
			"--set", "tags=new-tag",
		})
		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.Regexp(t, `tags:.*new-tag`, out)
		assert.NotRegexp(t, `env-prod`, out)
		assert.NotRegexp(t, `team-core`, out)

		// Edit: add a tag.
		r.Run(t, []string{
			"unikraft", "volume", "edit", "test-" + volName,
			"--output", "quiet",
			"--add", "tags=added-tag",
		})
		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.Regexp(t, `tags:.*new-tag`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		// Edit: del a tag.
		r.Run(t, []string{
			"unikraft", "volume", "edit", "test-" + volName,
			"--output", "quiet",
			"--del", "tags=new-tag",
		})
		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.NotRegexp(t, `new-tag`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("delete-lock", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--name", "test-" + volName,
			"--size", "10",
			"--metro", r.MetroName,
		})
		r.Run(t, []string{
			"unikraft", "volume", "edit", "test-" + volName,
			"--output", "quiet",
			"--delete-lock=true",
		})

		out := r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName, "-f", "+delete-lock"})
		assert.Regexp(t, `delete-lock:\s+true`, out)

		out = r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName}, integ.ExpectFail())
		assert.Regexp(t, `(?i)deletion protection`, out)

		r.Run(t, []string{
			"unikraft", "volume", "edit", "test-" + volName,
			"--output", "quiet",
			"--delete-lock=false",
		})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})
}
