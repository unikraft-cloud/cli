// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"context"
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/internal/config"
	integ "unikraft.com/cli/internal/integration"
	"unikraft.com/cli/internal/multimetro"
)

//go:embed testdata/counter_server.py
var counterServerPy string

// sharedCounter serves a counter over HTTP. GET /count reads the count and
// POST /increment changes it; COUNTER_FILE selects the file that holds it.
var sharedCounter = &integ.SharedImage{
	Name: "counter-e2e",
	Files: map[string]string{
		"Dockerfile": "FROM python:3.12-slim\nCOPY server.py /app/server.py\n",
		"server.py":  counterServerPy,
		"Kraftfile": `spec: v0.7
name: counter-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["python3", "/app/server.py"]
`,
	},
}

// followArgs counts the boots in /data/n, prints the count, then waits. The
// instance stays running while the test reads the output of the boot.
const followArgs = `runtime.args=["sh","-c","n=$(cat /data/n 2>/dev/null || echo 0); n=$((n+1)); echo $n > /data/n; echo starting $n; sleep 30s"]`

const sandboxPluginRom = "plugins/sandbox:staging"

// tunnelProxyUUIDs queries the platform directly (bypassing the CLI's
// resource partition, which never tracks the tunnel command's internal proxy
// instance since it's created via the raw platform client rather than
// through a partitioned resource) for every currently-running tunnel-service
// instance in the given metro.
func tunnelProxyUUIDs(t *testing.T, cfg *config.Config, metro string) map[string]struct{} {
	t.Helper()
	ctx := config.WithConfig(t.Context(), cfg)
	g, err := multimetro.NewClient(ctx)
	require.NoError(t, err)

	uuids := make(map[string]struct{})
	err = group.DoMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) error {
		resp, err := c.GetInstances(ctx, nil, platform.GetInstancesOpts{Details: new(true)})
		if err != nil {
			return err
		}
		if resp == nil || resp.Data == nil {
			return nil
		}
		for _, inst := range resp.Data.Instances {
			if strings.Contains(inst.Image, "utils/tunnel") {
				uuids[inst.Uuid] = struct{}{}
			}
		}
		return nil
	})
	require.NoError(t, err)
	return uuids
}

func TestInstances(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		// TODO: Add 'prod' back when it runs platform version 13. Older
		// versions send a duplicate "status" member that breaks every wait.
		// See https://github.com/unikraft-cloud/platform/pull/937
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		out := r.Run(t, []string{"unikraft", "instance", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))

		out = r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=A=1",
			"--set", "runtime.env=B=2",
			"--set", "runtime.env=C=3",
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

	t.Run("create-env", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		// Commas and further "=" signs belong to the value, so each variable
		// needs its own flag.
		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--name", "test-" + instName,
			"--metro", r.Config.MetroName,
			"--image", "nginx:latest",
			"--memory", "128",
			"--vcpus", "1",
			"--set", "autostart=false",
			"-e", "LIST=a,b,c",
			"-e", "PAIRS=k1=v1,k2=v2",
			"--set", "runtime.env=VIA_SET=x,y",
		})
		assert.Regexp(t, `LIST:\s+a,b,c`, out)
		assert.Regexp(t, `PAIRS:\s+k1=v1,k2=v2`, out)
		assert.Regexp(t, `VIA_SET:\s+x,y`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"-e", "LIST=d,e",
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `LIST:\s+d,e`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("create-annotations", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--name", "test-" + instName,
			"--metro", r.Config.MetroName,
			"--image", "nginx:latest",
			"--memory", "128",
			"--vcpus", "1",
			"--set", "autostart=false",
			"--annotation", "my-key=value1",
			"--annotation", "example.com/annotation2=value2",
			"--set", "annotations=test.unikraft.com/via-set=value3",
		})
		assert.Regexp(t, `my-key:\s+value1`, out)
		assert.Regexp(t, `example\.com/annotation2:\s+value2`, out)
		assert.Regexp(t, `test\.unikraft\.com/via-set:\s+value3`, out)

		// The shortcut flag maps to --set, which replaces the whole map.
		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--annotation", "my-key=replaced",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `my-key:\s+replaced`, out)
		assert.NotRegexp(t, `example\.com/annotation2`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--add", "annotations=added-key=added-value",
			"--add", "annotations=my-key=overwritten",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `added-key:\s+added-value`, out)
		assert.Regexp(t, `my-key:\s+overwritten`, out)

		// del takes bare keys, not key=value pairs.
		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--del", "annotations=added-key",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.NotRegexp(t, `added-key`, out)
		assert.Regexp(t, `my-key:\s+overwritten`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("annotations-guest", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		image := integ.Busybox.Build(t, r)

		r.Run(t, []string{
			"unikraft", "run",
			"--name", "test-" + instName,
			"--metro", r.Config.MetroName,
			"--output", "quiet",
			"--image", image,
			"--args", "cat /sys/class/uio/uio0/device/startdata",
			"--annotation", "my-key=value1",
			"--annotation", "example.com/annotation2=value2",
		})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==stopped", "test-" + instName})

		out := r.Run(t, []string{"unikraft", "instance", "logs", "test-" + instName})
		startdata := regexp.MustCompile(`"annotations":({[^}]*})`).FindStringSubmatch(out)
		require.Len(t, startdata, 2, "no annotations object in startdata: %s", out)

		var annotations map[string]string
		require.NoError(t, json.Unmarshal([]byte(startdata[1]), &annotations))
		assert.Equal(t, map[string]string{
			"my-key":                  "value1",
			"example.com/annotation2": "value2",
		}, annotations)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("create-oom", func(t *testing.T) {
		// TODO: Add 'stable' back when it runs platform version 13. Older
		// versions send a duplicate "status" member that breaks every wait.
		// See https://github.com/unikraft-cloud/platform/pull/937
		r := runner(t, true, []string{staging})
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=24Mib",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})
		assert.Regexp(t, `state:\s+stopped`, out)
		assert.Regexp(t, `stop:`, out)
		assert.Regexp(t, `reason:.*(page fault|out of memory)`, out)
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("connect", func(t *testing.T) {
		// TODO: Add 'prod' back when it runs platform version 13. Older
		// versions send a duplicate "status" member that breaks every wait.
		// See https://github.com/unikraft-cloud/platform/pull/937
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		domainName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=A=1",
			"--set", "runtime.env=B=2",
			"--set", "runtime.env=C=3",
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

	t.Run("getting-started", func(t *testing.T) {
		// TODO: Add 'prod' back when it runs platform version 13. Older
		// versions send a duplicate "status" member that breaks every wait.
		// See https://github.com/unikraft-cloud/platform/pull/937
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "run",
			"--name", "test-" + instName,
			"--metro", r.Config.MetroName,
			"--publish", "443:8080/http+tls",
			"--scale-to-zero", "policy=on,cooldown-time=1000",
			"--image", "nginx:latest",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.Regexp(t, `state:\s+(running|starting)`, out)
		assert.Regexp(t, `policy:\s+on`, out)
		assert.Regexp(t, `service:`, out)

		out = r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)

		r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==running", "--until", "state==standby", "test-" + instName})

		body := integ.HTTPGet(t, "https://"+fqdn)
		assert.Contains(t, body, "Welcome to nginx!")

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("start-stop", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=A=1",
			"--set", "runtime.env=B=2",
			"--set", "runtime.env=C=3",
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

	t.Run("start-follow", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		volName := uniq()
		baseImage := integ.Busybox.Build(t, r)

		// Volume provides persistent counter across boots.
		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		// On each boot, increment /data/n and echo "starting N".
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
			"--set", followArgs,
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

		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("restart-follow", func(t *testing.T) {
		// TODO: Add 'stable' back when it runs platform version 13. Older
		// versions send a duplicate "status" member that breaks every wait.
		// See https://github.com/unikraft-cloud/platform/pull/937
		r := runner(t, true, []string{staging})
		instName := uniq()
		volName := uniq()
		baseImage := integ.Busybox.Build(t, r)

		// Volume provides persistent counter across boots.
		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		// On each boot, increment /data/n and echo "starting N".
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
			"--set", followArgs,
		})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		// Second boot via restart --follow: output must contain "starting 2" only.
		// Restart incurs stop+start overhead on top of the boot itself, so it
		// needs more headroom than a plain start-follow to avoid flaking.
		out := r.Run(t, []string{
			"unikraft", "instance", "restart",
			"--follow",
			"test-" + instName,
		}, integ.WithTimeout(15*time.Second), integ.AllowFail())
		assert.Contains(t, out, "starting 2")
		assert.NotContains(t, out, "starting 1")

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("edit", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.args=before,first",
			"--set", "runtime.env=A=1",
			"--set", "runtime.env=B=2",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "image=redis:latest",
			"--set", "runtime.args=after,second",
			"--set", "runtime.env=A=3",
			"--set", "runtime.env=B=4",
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
		r := runner(t, true, []string{staging, stable})
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
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("volume-inline", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
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
		r := runner(t, true, []string{staging, stable})
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
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
		r.Run(t, []string{"unikraft", "service", "delete", "test-" + svcName})
	})

	t.Run("rom-attach", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
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
		r := runner(t, true, []string{staging, stable})
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
		r := runner(t, true, []string{staging, stable})
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
			"--del", "roms=myrom",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.NotRegexp(t, `roms:`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("plugin-create", func(t *testing.T) {
		r := runner(t, true, []string{staging})
		instName := uniq()

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--plugin", "name=sandbox,rom=" + sandboxPluginRom,
		})
		assert.Regexp(t, `name:\s+sandbox`, out)
		assert.Regexp(t, `rom:\s+\S*plugins/sandbox`, out)

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `name:\s+sandbox`, out)
		assert.Regexp(t, `rom:\s+\S*plugins/sandbox`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("plugin-config", func(t *testing.T) {
		r := runner(t, true, []string{staging})
		instName := uniq()
		const config = `{"level":"debug","tags":["a","b"],"msg":"p,q"}`

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--plugin", `name=sandbox,rom=` + sandboxPluginRom + `,config=` + config,
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName, "--output", "json"})
		var instances []struct {
			Plugins []struct {
				Name   string          `json:"name"`
				Rom    string          `json:"rom"`
				Config json.RawMessage `json:"config"`
			} `json:"plugins"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &instances))
		require.Len(t, instances, 1)
		require.Len(t, instances[0].Plugins, 1)
		assert.Equal(t, "sandbox", instances[0].Plugins[0].Name)
		assert.Contains(t, instances[0].Plugins[0].Rom, "plugins/sandbox")
		assert.JSONEq(t, config, string(instances[0].Plugins[0].Config))

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("plugin-edit", func(t *testing.T) {
		r := runner(t, true, []string{staging})
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
		})

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--plugin", "name=first,rom=" + sandboxPluginRom,
		})
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `name:\s+first`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--add", "plugins=name=second,rom=" + sandboxPluginRom,
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `name:\s+first`, out)
		assert.Regexp(t, `name:\s+second`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--del", "plugins=first",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.NotRegexp(t, `name:\s+first`, out)
		assert.Regexp(t, `name:\s+second`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--plugin", "name=only,rom=" + sandboxPluginRom,
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `name:\s+only`, out)
		assert.NotRegexp(t, `name:\s+second`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("plugin-invalid", func(t *testing.T) {
		tests := []struct {
			name   string
			plugin string
			want   string
		}{
			{"missing-name", "rom=" + sandboxPluginRom, "must specify name= for a plugin"},
			{"missing-rom", "name=sandbox", `must specify rom= for plugin "sandbox"`},
			{"config-only", `config={"level":"debug"}`, "must specify name= for a plugin"},
			{"invalid-json", "name=sandbox,rom=" + sandboxPluginRom + ",config={oops}", "config is not valid JSON"},
			{"truncated-json", `name=sandbox,rom=` + sandboxPluginRom + `,config={"level":"debug"`, `missing "}"`},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				r := runner(t, false, []string{staging, stable})
				out := r.Run(t, []string{
					"unikraft", "instance", "create",
					"--set", "name=test-plugin-invalid",
					"--set", "metro=fra",
					"--set", "image=nginx:latest",
					"--plugin", tt.plugin,
				}, integ.ExpectFail())
				assert.Contains(t, out, tt.want)
			})
		}
	})

	t.Run("volume-add", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
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
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("volume-del", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
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
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("autostart", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
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

	t.Run("create-waits-for-running", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
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
		assert.Regexp(t, `state:\s+running`, out)
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("suspend", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
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
		r := runner(t, true, []string{staging, stable})
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
		r := runner(t, true, []string{staging, stable})
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
		r := runner(t, true, []string{staging, stable})
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

	t.Run("gpu", func(t *testing.T) {
		t.Skip("gpus are not enabled in testing envs")

		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "type=full",
			"--set", "resources.gpus=1",
		})
		assert.Regexp(t, `type:\s+full`, out)
		assert.Regexp(t, `gpus:\s+1`, out)

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `type:\s+full`, out)
		assert.Regexp(t, `gpus:\s+1`, out)
		assert.Regexp(t, `model:\s+\S+`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("tags", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		// Create instance with tags.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--tag", "env-prod",
			"--tag", "team-core",
		})

		// Verify tags appear in inspect output.
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `tags:.*env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)

		// Filter by tag.
		out = r.Run(t, []string{"unikraft", "instance", "list", "--filter", "tags.*==env-prod"})
		assert.Contains(t, out, "test-"+instName)

		out = r.Run(t, []string{"unikraft", "instance", "list", "--filter", "tags.*==no-match"})
		assert.NotContains(t, out, "test-"+instName)

		// Edit: set (replace all tags).
		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "tags=new-tag",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `tags:.*new-tag`, out)
		assert.NotRegexp(t, `env-prod`, out)
		assert.NotRegexp(t, `team-core`, out)

		// Edit: add a tag.
		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--add", "tags=added-tag",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `tags:.*new-tag`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		// Edit: del a tag.
		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--del", "tags=new-tag",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.NotRegexp(t, `new-tag`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	// Each shortcut flag carries exactly one element, repeated for more.
	t.Run("repeated-shortcut-flags", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		volName := uniq()
		domainName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--name", "test-" + volName,
			"--metro", r.Config.MetroName,
			"--size", "10",
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--name", "test-" + instName,
			"--metro", r.Config.MetroName,
			"--image", "nginx:latest",
			"--memory", "128",
			"--vcpus", "1",
			"--set", "autostart=false",
			"--tag", "env-prod",
			"--tag", "team-core",
			"--publish", "443:8080/tls+http",
			"--publish", "80:8080/http",
			"--domain", domainName + ".unikraft.example",
			"--volume", "test-" + volName + ":/data",
			"--feature", "delete-on-stop",
		})

		// Ports and features are invisible fields on an instance, so a
		// successful create is all --publish/--feature can be checked by here;
		// service_test.go covers ports through the service group.
		assert.Regexp(t, `tags:.*env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)
		assert.Contains(t, out, domainName)
		assert.Contains(t, out, "test-"+volName)

		// An exact-match filter proves the two tags were not joined into one
		// literal "env-prod,team-core" value.
		out = r.Run(t, []string{"unikraft", "instance", "list", "--filter", "tags.*==env-prod"})
		assert.Contains(t, out, "test-"+instName)
		out = r.Run(t, []string{"unikraft", "instance", "list", "--filter", "tags.*==team-core"})
		assert.Contains(t, out, "test-"+instName)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("delete-lock", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
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
		})

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "delete-lock=true",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName, "-f", "+delete-lock"})
		assert.Regexp(t, `delete-lock:\s+true`, out)

		out = r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName}, integ.ExpectFail())
		assert.Regexp(t, `(?i)deletion protection`, out)

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `name:\s+test-`+instName, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "delete-lock=false",
		})

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName}, integ.ExpectFail())
		assert.Regexp(t, `not found`, out)
	})

	t.Run("pull-policy", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		imageTag := "test-" + uniq()
		warmName := uniq()
		ifNotPresentName := uniq()
		alwaysName := uniq()

		image := r.Config.Profile.Organization + "/pull-policy-e2e:" + imageTag

		dir := t.TempDir()

		// Build and push v1: short-lived instance that prints a known marker.
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("v1", 0o755),
			fstest.CreateFile("v1/Dockerfile", []byte(`
FROM busybox:latest
RUN echo pull-policy-v1 > /marker.txt
`), 0o644),
			fstest.CreateFile("v1/Kraftfile", []byte(`
spec: v0.7
name: pull-policy-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/marker.txt"]
`), 0o644),
		).Apply(dir))
		r.Run(t, []string{"unikraft", "build", "v1", "--output", image}, integ.WithWorkDir(dir))

		// Warm the node cache: run v1, wait for it to stop, check output.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + warmName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})
		r.Run(t, []string{
			"unikraft", "--timeout", "30s", "instance", "wait",
			"--until", "state==stopped", "test-" + warmName,
		})
		out := r.Run(t, []string{"unikraft", "instance", "logs", "test-" + warmName})
		assert.Contains(t, out, "pull-policy-v1")
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + warmName})

		// Build and push v2 under the same tag: different marker.
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("v2", 0o755),
			fstest.CreateFile("v2/Dockerfile", []byte(`
FROM busybox:latest
RUN echo pull-policy-v2 > /marker.txt
`), 0o644),
			fstest.CreateFile("v2/Kraftfile", []byte(`
spec: v0.7
name: pull-policy-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/marker.txt"]
`), 0o644),
		).Apply(dir))
		r.Run(t, []string{"unikraft", "build", "v2", "--output", image}, integ.WithWorkDir(dir))

		// if_not_present: node already has v1 cached under this tag; must NOT pull v2.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + ifNotPresentName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--pull-policy", "if_not_present",
		})
		r.Run(t, []string{
			"unikraft", "--timeout", "30s", "instance", "wait",
			"--until", "state==stopped", "test-" + ifNotPresentName,
		})
		out = r.Run(t, []string{"unikraft", "instance", "logs", "test-" + ifNotPresentName})
		assert.Contains(t, out, "pull-policy-v1")
		assert.NotContains(t, out, "pull-policy-v2")
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + ifNotPresentName})

		// always: must pull fresh v2 regardless of cache.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + alwaysName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--pull-policy", "always",
		})
		r.Run(t, []string{
			"unikraft", "--timeout", "30s", "instance", "wait",
			"--until", "state==stopped", "test-" + alwaysName,
		})
		out = r.Run(t, []string{"unikraft", "instance", "logs", "test-" + alwaysName})
		assert.Contains(t, out, "pull-policy-v2")
		assert.NotContains(t, out, "pull-policy-v1")
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + alwaysName})

		r.Run(t, []string{"unikraft", "image", "delete", image})
	})

	t.Run("replicas", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		out := r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "template={{ .name }}",
			"--name", "test-" + instName,
			"--metro", r.Config.MetroName,
			"--image", "nginx:latest",
			"--memory", "128",
			"--vcpus", "1",
			"--replicas", "2",
		})
		instances := strings.Fields(out)
		require.Len(t, instances, 3)
		assert.Len(t, map[string]struct{}{
			instances[0]: {},
			instances[1]: {},
			instances[2]: {},
		}, 3)

		out = r.Run(t, append([]string{"unikraft", "instance", "inspect"}, instances...))
		assert.Regexp(t, `image:\s+nginx`, out)

		r.Run(t, append([]string{"unikraft", "instance", "delete"}, instances...))
	})

	t.Run("watch-timeout", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		r.Run(t, []string{"unikraft", "--timeout=1s", "instance", "ls", "-w"}, integ.AllowFail())
	})

	t.Run("watch-no-timeout", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

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

	t.Run("tunnel", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		instName2 := uniq()

		// Create two nginx instances without a public service, in the same
		// metro, so a single tunnel invocation with two targets exercises the
		// proxy's port-grouping/sequential-assignment logic for real.
		for _, name := range []string{instName, instName2} {
			r.Run(t, []string{
				"unikraft", "instance", "create",
				"--output", "quiet",
				"--set", "name=test-" + name,
				"--set", "metro=" + r.Config.MetroName,
				"--set", "image=nginx:latest",
				"--set", "autostart=true",
				"--set", "resources.memory=128",
				"--set", "resources.vcpus=1",
			})
		}
		r.Run(t, []string{
			"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "60s",
			"test-" + instName, "test-" + instName2,
		})

		before := tunnelProxyUUIDs(t, r.Config.Config, r.Config.MetroName)

		// Use metro/instance:port/tcp syntax for the tunnel targets.
		tunnel := r.StartBackground(t,
			[]string{
				"unikraft", "instance", "tunnel",
				"18081:" + r.Config.MetroName + "/test-" + instName + ":8080/tcp",
				"18082:" + r.Config.MetroName + "/test-" + instName2 + ":8080/tcp",
			},
			"127.0.0.1:18081",
			0,
		)

		// Verify that we can reach both instances through the tunnel.
		body := integ.HTTPGet(t, "http://127.0.0.1:18081")
		assert.Regexp(t, "Thank you for using nginx.", body)
		body = integ.HTTPGet(t, "http://127.0.0.1:18082")
		assert.Regexp(t, "Thank you for using nginx.", body)

		// Identify the proxy instance the tunnel command created, so we can
		// confirm below that tearing down the tunnel actually deletes it
		// (a regression in Close() would otherwise go unnoticed, since the
		// CLI's own partitioned "instance list"/"inspect" never see this
		// instance either way).
		during := tunnelProxyUUIDs(t, r.Config.Config, r.Config.MetroName)
		var proxyUUID string
		for u := range during {
			if _, ok := before[u]; ok {
				continue
			}
			require.Empty(t, proxyUUID, "expected exactly one new tunnel proxy instance, found multiple")
			proxyUUID = u
		}
		require.NotEmpty(t, proxyUUID, "expected a new tunnel proxy instance to appear while the tunnel is running")

		tunnel.Stop()

		after := tunnelProxyUUIDs(t, r.Config.Config, r.Config.MetroName)
		_, stillThere := after[proxyUUID]
		assert.False(t, stillThere, "tunnel proxy instance %s was not deleted after the tunnel was torn down", proxyUUID)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName, "test-" + instName2})
	})

	t.Run("branch", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		branchName := uniq()
		domainName := uniq()
		domainBranch := uniq()
		image := sharedCounter.Build(t, r)

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=256",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainName,
		})
		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 0`)

		// Branch the running instance.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--branch", "test-" + instName,
			"--set", "name=test-branch-" + branchName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "autostart=true",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainBranch,
		})
		out = r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-branch-" + branchName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdnBranch := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-branch-" + branchName})
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 0`)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName, "test-branch-" + branchName})
	})

	// branch-state verifies that --branch preserves current in-memory state and
	// that the original and branched instances are fully independent. It builds
	// a counter HTTP server, increments to 5, branches, verifies the branched
	// instance has counter=5, then mutates each independently.
	t.Run("branch-state", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		branchName := uniq()
		domainName := uniq()
		domainBranch := uniq()
		image := sharedCounter.Build(t, r)

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=256",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainName,
		})
		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		// Increment counter to 5.
		for range 5 {
			integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		}
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		// Branch the running instance.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--branch", "test-" + instName,
			"--set", "name=test-branch-" + branchName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "autostart=true",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainBranch,
		})
		out = r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-branch-" + branchName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdnBranch := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-branch-" + branchName})

		// Branched counter should also be at 5.
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 5`)

		// Increment branched by 10 → 15.
		integ.HTTPPost(t, "https://"+fqdnBranch+"/increment", "application/json", `{"delta":10}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 15`)

		// Original should still be at 5 (unaffected by branch).
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		// Increment original by 1 → 6.
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 6`)

		// Branched should still be at 15 (unaffected by original).
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 15`)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName, "test-branch-" + branchName})
	})

	// stop-disk-reset verifies a plain stop/start also resets the root disk (by design, not just a --branch gap).
	t.Run("stop-disk-reset", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		domainName := uniq()
		image := sharedCounter.Build(t, r)

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=256",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainName,
		})
		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		for range 5 {
			integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		}
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		r.Run(t, []string{"unikraft", "instance", "stop", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==stopped", "--timeout", "30s", "test-" + instName})

		r.Run(t, []string{"unikraft", "instance", "start", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		// Root disk is ephemeral per boot; only attached volumes persist.
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 0`)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	// branch-stopped verifies --branch works when the source is stopped. The
	// counter is kept on an attached volume rather than the root disk, since
	// the root disk never survives a stop (see stop-disk-reset) but branching
	// copies attached volumes.
	t.Run("branch-stopped", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		branchName := uniq()
		volName := uniq()
		domainName := uniq()
		domainBranch := uniq()
		image := sharedCounter.Build(t, r)

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
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=256",
			"--set", "resources.vcpus=1",
			"--set", "runtime.env=COUNTER_FILE=/data/counter.txt",
			"--set", "volumes=test-" + volName + ":/data",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainName,
		})
		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		// Increment counter to 5, then stop the instance.
		for range 5 {
			integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		}
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		r.Run(t, []string{"unikraft", "instance", "stop", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==stopped", "--timeout", "30s", "test-" + instName})

		// Branch the stopped instance.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--branch", "test-" + instName,
			"--set", "name=test-branch-" + branchName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "autostart=true",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainBranch,
		})
		out = r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-branch-" + branchName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdnBranch := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-branch-" + branchName})

		// Branched counter should read 5, carried over by the copied volume.
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 5`)

		// The branch's volume must be an independent copy, not a shared
		// reference: mutating it and restarting the (still-stopped) source
		// must not affect the source's own count.
		integ.HTTPPost(t, "https://"+fqdnBranch+"/increment", "application/json", `{"delta":10}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 15`)

		r.Run(t, []string{"unikraft", "instance", "start", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 15`)

		// The branch's own volume is unnamed and cleaned up by the partition's
		// resource tracking; only the explicitly created source volume needs
		// to be deleted here.
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName, "test-branch-" + branchName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	// branch-template verifies --branch works when the source is a template.
	t.Run("branch-template", func(t *testing.T) {
		// branch_from can't resolve template names/UUIDs on the backend yet.
		t.Skip("branching from a template is not resolvable via branch_from on the backend yet")
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		branchName := uniq()
		domainName := uniq()
		domainBranch := uniq()
		image := sharedCounter.Build(t, r)

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=256",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainName,
		})
		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		// Increment counter to 5, then stop the instance and convert it into a template.
		for range 5 {
			integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		}
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		r.Run(t, []string{"unikraft", "instance", "stop", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==stopped", "--timeout", "30s", "test-" + instName})

		out = r.Run(t, []string{
			"unikraft", "instance", "template", "create", "test-" + instName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		// Branch the template.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--branch", templateName,
			"--set", "name=test-branch-" + branchName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "autostart=true",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainBranch,
		})
		out = r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-branch-" + branchName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdnBranch := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-branch-" + branchName})

		// Assumes template create snapshots the disk rather than reading a reset instance.
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 5`)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName, "test-branch-" + branchName})
		r.Run(t, []string{"unikraft", "instance", "template", "delete", templateName})
	})

	t.Run("autokill", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
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
			"--autokill", "time=5s",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `autokill:`, out)
		assert.Regexp(t, `time:\s+5s`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--autokill", `time=10s,num-requests=100`,
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `autokill:`, out)
		assert.Regexp(t, `time:\s+10s`, out)
		assert.Regexp(t, `num-requests:\s+100`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})
}
