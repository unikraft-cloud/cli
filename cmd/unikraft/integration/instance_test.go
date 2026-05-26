// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestInstances(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		out := r.Run(t, []string{"unikraft", "instance", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))

		out = r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=A=1,B=2,C=3",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})
		assert.Regexp(t, `name:\s+test-`, out)
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.Regexp(t, `memory:\s+128`, out)
		assert.Regexp(t, `state:\s+(running|starting)`, out)
		assert.Regexp(t, `A:\s+1`, out)

		out = r.Run(t, []string{"unikraft", "instance", "list"})
		assert.Regexp(t, `test-.*nginx`, out)

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.Regexp(t, `memory:\s+128`, out)
		assert.Regexp(t, `state:\s+(running|starting)`, out)

		out = r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		assert.Regexp(t, `test-`, out)

		out = r.Run(t, []string{"unikraft", "instance", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))
	})

	t.Run("create-oom", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=16Mib",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})
		assert.Regexp(t, `state:\s+stopped`, out)
		assert.Regexp(t, `stop:`, out)
		assert.Regexp(t, `reason:.*(page fault|out of memory)`, out)
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("create-stopped", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})
		assert.Regexp(t, `state:\s+stopped`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("create-name-too-long", func(t *testing.T) {
		r := runner(t, true)

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=" + strings.Repeat("a", 64),
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		}, integ.ExpectFail())
		assert.Regexp(t, `(?i)invalid`, out)
	})

	t.Run("create-name-trailing-hyphen", func(t *testing.T) {
		r := runner(t, true)

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + uniq() + "-",
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		}, integ.ExpectFail())
		assert.Regexp(t, `(?i)invalid`, out)
	})

	t.Run("create-name-special-chars", func(t *testing.T) {
		r := runner(t, true)

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + uniq() + "!@#",
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		}, integ.ExpectFail())
		assert.Regexp(t, `(?i)invalid`, out)
	})

	t.Run("create-name-uppercase", func(t *testing.T) {
		r := runner(t, true)
		instName := "TEST-" + strings.ToUpper(uniq())

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", strings.ToLower(instName)})
		assert.Regexp(t, `name:\s+`+strings.ToLower(instName), out)

		r.Run(t, []string{"unikraft", "instance", "delete", strings.ToLower(instName)})
	})

	t.Run("create-memory-negative", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=-16",
			"--set", "resources.vcpus=1",
		}, integ.ExpectFail())
		assert.Regexp(t, `'memory_mb' must be in the range`, out)
	})

	t.Run("create-memory-too-large", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=10000000000",
			"--set", "resources.vcpus=1",
		}, integ.ExpectFail())
		assert.Regexp(t, `(?i)memory|range|must be`, out)
	})

	t.Run("create-nonexistent-service", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--service", "nonexistent-" + uniq(),
		}, integ.ExpectFail())
		assert.Regexp(t, `(?i)No service group with name`, out)
	})

	t.Run("create-env-unicode", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=HÉLLO=wörld",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `HÉLLO:\s+wörld`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("connect", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		domainName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=A=1,B=2,C=3",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainName,
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)

		r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==running", "test-" + instName})

		body := integ.HTTPGet(t, "https://"+fqdn)
		assert.Contains(t, body, "Welcome to nginx!")

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("start-stop", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=A=1,B=2,C=3",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{"unikraft", "instance", "stop", "test-" + instName})
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+(stopped|stopping)\b`, out)

		r.Run(t, []string{"unikraft", "instance", "start", "test-" + instName})
		// TODO: start doesn't actually wait to start; re-enable once it does.
		// out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		// assert.Regexp(t, `state:\s+running`, out)

		r.Run(t, []string{"unikraft", "instance", "edit", "test-" + instName, "--set", "state=stopped"})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+(stopped|stopping)\b`, out)

		r.Run(t, []string{"unikraft", "instance", "edit", "test-" + instName, "--set", "state=running"})
		// TODO: start doesn't actually wait to start; re-enable once it does.
		// out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		// assert.Regexp(t, `state:\s+running`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("restart", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		r.Run(t, []string{"unikraft", "instance", "restart", "test-" + instName})

		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+running`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("start-follow", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()
		imageTag := uniq()

		baseImagePrefix := r.Config.Profile.Organization + "/busybox-start-follow-e2e"
		baseImage := baseImagePrefix + ":" + imageTag

		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("base", 0o755),
			fstest.CreateFile("base/Dockerfile", []byte(`FROM busybox:latest`), 0o644),
			fstest.CreateFile("base/Kraftfile", []byte(`
spec: v0.7
name: busybox-start-follow-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/rom/hello.txt"]
`), 0o644),
		).Apply(dir))

		r.Run(t, []string{"unikraft", "build", "base", "--output", baseImage}, integ.WithWorkDir(dir))

		// Volume provides persistent counter across boots.
		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		// On each boot, increment /data/n and echo "starting N".
		script := `n=$(cat /data/n 2>/dev/null || echo 0); n=$((n+1)); echo $n > /data/n; echo starting $n; sleep 30`
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + baseImage,
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/data",
			"--set", `runtime.args=["sh","-c","` + script + `"]`,
		})

		// First boot ("starting 1"): start, wait running, then stop.
		r.Run(t, []string{"unikraft", "instance", "start", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "stop", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==stopped", "--timeout", "30s", "test-" + instName})

		// Second boot via start --follow: output must contain "starting 2" only.
		out := r.Run(t, []string{
			"unikraft", "instance", "start",
			"--follow",
			"test-" + instName,
		}, integ.WithTimeout(5*time.Second), integ.AllowFail())
		assert.Contains(t, out, "starting 2")
		assert.NotContains(t, out, "starting 1")

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("restart-follow", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()
		imageTag := uniq()

		baseImagePrefix := r.Config.Profile.Organization + "/busybox-restart-follow-e2e"
		baseImage := baseImagePrefix + ":" + imageTag

		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("base", 0o755),
			fstest.CreateFile("base/Dockerfile", []byte(`FROM busybox:latest`), 0o644),
			fstest.CreateFile("base/Kraftfile", []byte(`
spec: v0.7
name: busybox-restart-follow-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/rom/hello.txt"]
`), 0o644),
		).Apply(dir))

		r.Run(t, []string{"unikraft", "build", "base", "--output", baseImage}, integ.WithWorkDir(dir))

		// Volume provides persistent counter across boots.
		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		// On each boot, increment /data/n and echo "starting N".
		script := `n=$(cat /data/n 2>/dev/null || echo 0); n=$((n+1)); echo $n > /data/n; echo starting $n; sleep 30`
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + baseImage,
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/data",
			"--set", `runtime.args=["sh","-c","` + script + `"]`,
		})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		// Second boot via restart --follow: output must contain "starting 2" only.
		out := r.Run(t, []string{
			"unikraft", "instance", "restart",
			"--follow",
			"test-" + instName,
		}, integ.WithTimeout(5*time.Second), integ.AllowFail())
		assert.Contains(t, out, "starting 2")
		assert.NotContains(t, out, "starting 1")

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("edit", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.args=before,first",
			"--set", "runtime.env=A=1,B=2",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "image=redis:latest",
			"--set", "runtime.args=after,second",
			"--set", "runtime.env=A=3,B=4",
			"--set", "resources.memory=256",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+(stopped|stopping)\b`, out)
		assert.Regexp(t, `image:\s+redis`, out)
		assert.Regexp(t, `memory:\s+256`, out)
		assert.Regexp(t, `args:`, out)
		assert.Regexp(t, `A:\s+3`, out)
		assert.Regexp(t, `B:\s+4`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("volume", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/mnt",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/mnt`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("volume-inline", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=:/data:size=20",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/data`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("shortcut-service-volume", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		svcName := uniq()
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "service", "create",
			"--output", "quiet",
			"--set", "name=test-" + svcName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "services=443:8080/tls+http",
		})

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--service", "test-" + svcName,
			"-v", "test-" + volName + ":/mnt",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/mnt`, out)
		assert.Regexp(t, `service:`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
		r.Run(t, []string{"unikraft", "service", "delete", "test-" + svcName})
	})

	t.Run("rom-attach", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("romdata", 0o755),
			fstest.CreateFile("romdata/hello.txt", []byte("Hello from ROM!\n"), 0o644),
		).Apply(dir))

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "roms=dir=romdata,at=/rom",
		}, integ.WithWorkDir(dir))

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/rom`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("rom-add", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("romdata1", 0o755),
			fstest.CreateFile("romdata1/hello.txt", []byte("Hello from ROM 1!\n"), 0o644),
			fstest.CreateDir("romdata2", 0o755),
			fstest.CreateFile("romdata2/goodbye.txt", []byte("Goodbye from ROM 2!\n"), 0o644),
		).Apply(dir))

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--rom", "dir=romdata1,at=/rom1,name=rom1",
		}, integ.WithWorkDir(dir))

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--add", "roms=dir=romdata2,at=/rom2,name=rom2",
		}, integ.WithWorkDir(dir))

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/rom1`, out)
		assert.Regexp(t, `at:\s+/rom2`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("rom-detach", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("romdata", 0o755),
			fstest.CreateFile("romdata/hello.txt", []byte("Hello from ROM!\n"), 0o644),
		).Apply(dir))

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--rom", "dir=romdata,at=/rom,name=myrom",
		}, integ.WithWorkDir(dir))

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--del", "roms=name=myrom",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.NotRegexp(t, `roms:`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("volume-add", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=10",
			"--set", "metro=" + r.Config.MetroName,
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--add", "volumes=test-" + volName + ":/data",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/data`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("volume-del", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=10",
			"--set", "metro=" + r.Config.MetroName,
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/data",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/data`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--del", "volumes=test-" + volName,
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.NotRegexp(t, `at:\s+/data`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("autostart", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+(running|starting)`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("suspend", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==running", "test-" + instName})

		// No scale-to-zero so state will show as stopped.
		r.Run(t, []string{"unikraft", "instance", "suspend", "test-" + instName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==stopped", "test-" + instName})
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+stopped`, out)
		assert.Regexp(t, `stop:`, out)
		assert.Regexp(t, `reason:.*user stop`, out)

		r.Run(t, []string{"unikraft", "instance", "start", "test-" + instName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==running", "test-" + instName})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+running`, out)
		assert.NotRegexp(t, `stop:`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("rm", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		// Create a running instance with --rm so it is auto-deleted
		// when stopped.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--rm",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==running", "test-" + instName})

		// Stop the instance so delete-on-stop removes it (deletion is async).
		r.Run(t, []string{"unikraft", "instance", "stop", "test-" + instName})

		// Verify the instance no longer exists.
		time.Sleep(time.Second)
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName}, integ.ExpectFail())
		assert.Regexp(t, `references not found`, out)
	})

	t.Run("add-domain", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		domainName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template={{ .service.name }}",
		})
		serviceName := strings.TrimSpace(out)

		r.Run(t, []string{
			"unikraft", "service", "edit", serviceName,
			"--output", "quiet",
			"--add", "domains=name=" + domainName,
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `service:`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("sched-priority", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--sched-priority", "medium",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `sched-priority:\s+medium`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--sched-priority", "high",
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `sched-priority:\s+high`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("watch-timeout", func(t *testing.T) {
		r := runner(t, true)
		r.Run(t, []string{"unikraft", "--timeout=1s", "instance", "ls", "-w"}, integ.AllowFail())
	})

	t.Run("watch-no-timeout", func(t *testing.T) {
		r := runner(t, true)

		done := make(chan error, 1)
		go func() {
			_, err := r.RunRaw(t, []string{"unikraft", "instance", "ls", "-w"}, integ.AllowFail())
			done <- err
		}()

		select {
		case err := <-done:
			t.Fatalf("expected command to still be running after 10 seconds, got: %v", err)
		case <-time.After(10 * time.Second):
			// Command still running
		}
	})
}
