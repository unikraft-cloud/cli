// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	integ "unikraft.com/cli/internal/integration"
)

func TestInstanceCheckpoints(t *testing.T) {
	t.Run("checkpoint", func(t *testing.T) {
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

		out := r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", "test-" + instName,
			"--output", "template={{ .name }}",
		})
		checkpointName := strings.TrimSpace(out)
		assert.NotEmpty(t, checkpointName)

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "list"})
		assert.Contains(t, out, checkpointName)

		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "checkpoint", "wait", "--until", "state==checkpoint", checkpointName})
		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "inspect", checkpointName})
		assert.Regexp(t, `state:\s+checkpoint`, out)

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "edit", checkpointName, "--set", "tags=env-dev"})
		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "inspect", checkpointName, "-f", "all"})
		assert.Contains(t, out, "env-dev")

		// The source instance's history lists its checkpoints.
		out = r.Run(t, []string{"unikraft", "instance", "history", "test-" + instName})
		assert.Contains(t, out, checkpointName)

		// A checkpoint's own history (ancestor checkpoints) is empty for a
		// freshly-created checkpoint; the command should still succeed and
		// render the table header.
		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "history", checkpointName})
		assert.Contains(t, out, "CREATED")

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName})

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "list", "--output", "quiet"})
		assert.NotContains(t, out, checkpointName)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	// checkpoint-bulk exercises multi-ref matching in InstanceCheckpoint.Get
	// and bulk delete across checkpoints created from different instances.
	t.Run("checkpoint-bulk", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		name1 := "test-" + uniq()
		name2 := "test-" + uniq()

		for _, name := range []string{name1, name2} {
			r.Run(t, []string{
				"unikraft", "instance", "create",
				"--output", "quiet",
				"--set", "name=" + name,
				"--set", "metro=" + r.Config.MetroName,
				"--set", "image=nginx:latest",
				"--set", "autostart=false",
				"--set", "resources.memory=128",
				"--set", "resources.vcpus=1",
			})
		}

		out1 := r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", name1,
			"--output", "template={{ .name }}",
		})
		out2 := r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", name2,
			"--output", "template={{ .name }}",
		})
		checkpoint1, checkpoint2 := strings.TrimSpace(out1), strings.TrimSpace(out2)
		assert.NotEmpty(t, checkpoint1)
		assert.NotEmpty(t, checkpoint2)
		assert.NotEqual(t, checkpoint1, checkpoint2)

		// Get() must resolve both checkpoints when queried together by
		// name, matching each result back to its requested ref.
		out := r.Run(t, []string{"unikraft", "instance", "checkpoint", "get", checkpoint1, checkpoint2, "--output", "quiet"})
		assert.Contains(t, out, checkpoint1)
		assert.Contains(t, out, checkpoint2)

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "list", "--output", "quiet"})
		assert.Contains(t, out, checkpoint1)
		assert.Contains(t, out, checkpoint2)

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpoint1, checkpoint2})
		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "list", "--output", "quiet"})
		assert.NotContains(t, out, checkpoint1)
		assert.NotContains(t, out, checkpoint2)

		r.Run(t, []string{"unikraft", "instance", "delete", name1, name2})
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

		out := r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", "test-" + instName,
			"--output", "template={{ .name }}",
		})
		checkpointName := strings.TrimSpace(out)
		assert.NotEmpty(t, checkpointName)

		r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "edit", checkpointName,
			"--output", "quiet",
			"--set", "delete-lock=true",
		})

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "inspect", checkpointName, "-f", "+delete-lock"})
		assert.Regexp(t, `delete-lock:\s+true`, out)

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName}, integ.ExpectFail())
		assert.Regexp(t, `(?i)deletion protection`, out)

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "inspect", checkpointName})
		assert.Contains(t, out, checkpointName)

		r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "edit", checkpointName,
			"--output", "quiet",
			"--set", "delete-lock=false",
		})

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName})

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "list", "--output", "quiet"})
		assert.NotContains(t, out, checkpointName)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	// volumes proves a checkpoint gets its own cloned copy of the source
	// instance's volumes, that the clone is a volume template rather than a
	// plain volume, and that the source volume is left untouched.
	t.Run("volumes", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "size=10",
		})

		out := r.Run(t, []string{
			"unikraft", "volume", "inspect", "test-" + volName,
			"--output", "template={{ .uuid }}",
		})
		srcVolUUID := strings.TrimSpace(out)
		assert.NotEmpty(t, srcVolUUID)

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

		out = r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", "test-" + instName,
			"--set", "wait-timeout=30s",
			"--output", "template={{ .name }}",
		})
		checkpointName := strings.TrimSpace(out)
		assert.NotEmpty(t, checkpointName)

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "inspect", checkpointName, "-f", "all"})
		assert.Regexp(t, `at:\s+/data`, out)

		// The checkpoint's volume is a clone, created without a name, so it is
		// only addressable by UUID.
		out = r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "inspect", checkpointName,
			"--output", "template=" + `{{ (index .volumes 0).uuid }}`,
		})
		ckptVolUUID := strings.TrimSpace(out)
		assert.NotEmpty(t, ckptVolUUID)
		assert.NotEqual(t, srcVolUUID, ckptVolUUID)

		out = r.Run(t, []string{"unikraft", "volume", "template", "inspect", ckptVolUUID})
		assert.Regexp(t, `state:\s+template`, out)
		r.Run(t, []string{"unikraft", "volume", "inspect", ckptVolUUID}, integ.ExpectFail())

		// The source volume is unaffected: still a plain volume.
		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName})
		assert.NotRegexp(t, `state:\s+template`, out)

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName})
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", "test-" + volName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("create-from-checkpoint", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		baseName := uniq()
		restoredName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-base-" + baseName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", "test-base-" + baseName,
			"--set", "wait-timeout=30s",
			"--output", "template={{ .name }}",
		})
		checkpointName := strings.TrimSpace(out)
		assert.NotEmpty(t, checkpointName)

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--checkpoint", checkpointName,
			"--set", "name=test-restored-" + restoredName,
			"--set", "metro=" + r.Config.MetroName,
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-restored-" + restoredName})
		assert.Regexp(t, `name:\s+test-restored-`, out)
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.Regexp(t, `memory:\s+128`, out)
		assert.Regexp(t, `vcpus:\s+1`, out)

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName})
		r.Run(t, []string{"unikraft", "instance", "delete", "test-base-" + baseName, "test-restored-" + restoredName})
	})

	// checkpoint-outlives-source proves a checkpoint's lifecycle is independent
	// of its source instance: the source is deleted first, and the still-alive
	// checkpoint is then used to restore a brand new instance.
	t.Run("checkpoint-outlives-source", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		baseName := uniq()
		restoredName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-base-" + baseName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", "test-base-" + baseName,
			"--set", "wait-timeout=30s",
			"--output", "template={{ .name }}",
		})
		checkpointName := strings.TrimSpace(out)
		assert.NotEmpty(t, checkpointName)

		// Delete the source instance. A cascade-delete regression would take
		// the checkpoint down with it.
		r.Run(t, []string{"unikraft", "instance", "delete", "test-base-" + baseName})

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "list", "--output", "quiet"})
		assert.Contains(t, out, checkpointName)

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "inspect", checkpointName})
		assert.Regexp(t, `state:\s+(checkpoint|starting)`, out)

		// Restore a new instance from the checkpoint after its source is gone.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--checkpoint", checkpointName,
			"--set", "name=test-restored-" + restoredName,
			"--set", "metro=" + r.Config.MetroName,
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-restored-" + restoredName})
		assert.Regexp(t, `name:\s+test-restored-`, out)

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName})
		r.Run(t, []string{"unikraft", "instance", "delete", "test-restored-" + restoredName})
	})

	// checkpoint-state verifies that a checkpoint preserves in-memory state and
	// that the restored instance is independent of the original. It builds a
	// counter HTTP server, increments to 3, takes a checkpoint, increments
	// further to 5, restores from the checkpoint (counter=3), then increments
	// each independently to prove isolation.
	t.Run("checkpoint-state", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()
		restoredName := uniq()
		domainName := uniq()
		domainRestored := uniq()
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

		// Increment counter to 3.
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 3`)

		// Take a checkpoint (counter=3).
		out = r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", "test-" + instName,
			"--set", "wait-timeout=30s",
			"--output", "template={{ .name }}",
		})
		checkpointName := strings.TrimSpace(out)
		assert.NotEmpty(t, checkpointName)

		// Increment original further to 5.
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		// Restore from checkpoint into a new instance.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--checkpoint", checkpointName,
			"--set", "name=test-restored-" + restoredName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "autostart=true",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainRestored,
		})
		out = r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-restored-" + restoredName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdnRestored := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-restored-" + restoredName})

		// Restored counter should be back at 3.
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnRestored+"/count"), `"count": 3`)

		// Increment restored independently by 10 → 13.
		integ.HTTPPost(t, "https://"+fqdnRestored+"/increment", "application/json", `{"delta":10}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnRestored+"/count"), `"count": 13`)

		// Original should still be at 5 (unaffected by restored).
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		// Increment original by 1 → 6.
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 6`)

		// Restored should still be at 13 (unaffected by original).
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnRestored+"/count"), `"count": 13`)

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName})
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName, "test-restored-" + restoredName})
	})
}
