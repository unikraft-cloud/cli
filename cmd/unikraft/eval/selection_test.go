// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"unikraft.com/cli/internal/eval"
)

// TestToolSelection checks which tool a model reaches for. Read-only
// prompts are judged by the first call, prompts that change something by
// the call the run stopped at, so a look-up before acting is allowed.
func TestToolSelection(t *testing.T) {
	r := selection(t)

	r.Prompt(t, "list-instances", "List my instances.", func(t *testing.T, res *eval.Result) {
		call := res.First(t)
		assert.Equal(t, "instance_list", call.Tool)
		call.Expect(t, eval.Args{"keys": eval.Absent})
	})

	r.Prompt(t, "memory-question", "How much memory does the instance web-1 have?", func(t *testing.T, res *eval.Result) {
		call := res.First(t)
		assert.Equal(t, "instance_list", call.Tool)
		call.Expect(t, eval.Args{"keys": eval.Keys{"web-1"}})
		assert.Len(t, res.Steps, 1)
	})

	r.Prompt(t, "filter-running-fra", "Show the instances that are running in fra.", func(t *testing.T, res *eval.Result) {
		call := res.First(t)
		assert.Equal(t, "instance_list", call.Tool)
		call.Expect(t, eval.Args{"filter": regexp.MustCompile(`state==running`)})
		call.Expect(t, eval.Args{"filter": regexp.MustCompile(`metro==fra`)})
	})

	r.Prompt(t, "logs", "Show me the logs of api-7.", func(t *testing.T, res *eval.Result) {
		assert.Equal(t, []string{"instance_logs"}, res.Tools())
		res.First(t).Expect(t, eval.Args{"key": eval.Keys{"api-7"}})
	})

	r.Prompt(t, "stop-one", "Stop web-1.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_control", call.Tool)
		call.Expect(t, eval.Args{"action": "stop", "keys": eval.Keys{"web-1"}})
	})

	r.Prompt(t, "restart-batch", "Restart web-1 and web-2.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_control", call.Tool)
		call.Expect(t, eval.Args{"action": "restart", "keys": eval.Keys{"web-1", "web-2"}})
	})

	r.Prompt(t, "suspend", "Suspend db-1 so it resumes on its next request.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_control", call.Tool)
		call.Expect(t, eval.Args{"action": "suspend", "keys": eval.Keys{"db-1"}})
	})

	r.Prompt(t, "delete-not-stop", "Remove web-1 entirely, I no longer need it.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_delete", call.Tool)
		call.Expect(t, eval.Args{"keys": eval.Keys{"web-1"}})
	})

	r.Prompt(t, "sandbox-persistent", "Create a sandbox named scratch with a persistent /data directory.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_create", call.Tool)
		call.Expect(t, eval.Args{
			"set.name":    "scratch",
			"set.image":   regexp.MustCompile(`official/(alpine|debian-slim)`),
			"set.plugins": regexp.MustCompile(`name=sandbox`),
			"set.volumes": regexp.MustCompile(`/data`),
		})
		call.Expect(t, eval.Args{"set.plugins": regexp.MustCompile(`plugins/sandbox`)})
		call.Expect(t, eval.Args{"set.plugins": regexp.MustCompile(`persist_path`)})
		call.Expect(t, eval.Args{"set.volumes": regexp.MustCompile(`size=`)})
	})

	r.Prompt(t, "sandbox-plain", "Spin up a sandbox I can run commands in.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_create", call.Tool)
		call.Expect(t, eval.Args{"set.plugins": regexp.MustCompile(`name=sandbox`)})
		assert.NotEqual(t, "false", call.Arg("set.autostart").String(), "the sandbox was left stopped")
	})

	r.Prompt(t, "create-stopped", "Create an instance named edge from nginx:latest but don't start it.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_create", call.Tool)
		call.Expect(t, eval.Args{"set.name": "edge", "set.image": "nginx:latest", "set.autostart": false})
	})

	r.Prompt(t, "create-default-start", "Create an instance named edge from nginx:latest.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_create", call.Tool)
		call.Expect(t, eval.Args{"set.name": "edge", "set.image": "nginx:latest"})
		assert.NotEqual(t, "false", call.Arg("set.autostart").String(), "the instance was left stopped")
	})

	r.Prompt(t, "edit-memory", "Give web-1 1 GiB of memory.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_edit", call.Tool)
		call.Expect(t, eval.Args{
			"key":                                eval.Keys{"web-1"},
			eval.Path("set", "resources.memory"): regexp.MustCompile(`^"?(1024|1Gi|1GiB|1G)"?$`),
		})
	})

	r.Prompt(t, "dry-run", "Preview the patch for setting web-1 to 2 vCPUs without applying it.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "instance_edit", call.Tool)
		call.Expect(t, eval.Args{
			"key":                               eval.Keys{"web-1"},
			"dry_run":                           true,
			eval.Path("set", "resources.vcpus"): 2,
		})
	})

	r.Prompt(t, "delete-volume", "Delete the volume foo.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "volume_delete", call.Tool)
		call.Expect(t, eval.Args{"keys": eval.Keys{"foo"}})
	})

	r.Prompt(t, "volume-create", "Create a 2 GiB volume named cache in fra.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "volume_create", call.Tool)
		call.Expect(t, eval.Args{
			"set.name":  "cache",
			"set.metro": "fra",
			"set.size":  regexp.MustCompile(`2Gi|2048`),
		})
	})

	r.Prompt(t, "image-contents", "What does the image nginx:latest contain?", func(t *testing.T, res *eval.Result) {
		assert.Equal(t, []string{"image_get"}, res.Tools())
		res.First(t).Expect(t, eval.Args{"keys": []string{"nginx:latest"}})
	})

	r.Prompt(t, "metros", "Which metros can I deploy to?", func(t *testing.T, res *eval.Result) {
		assert.Equal(t, "metro_list", res.First(t).Tool)
	})

	r.Prompt(t, "sandbox-exec", "Run `ls /data` inside the sandbox scratch.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "sandbox_exec", call.Tool)
		call.Expect(t, eval.Args{"key": "scratch", "command": regexp.MustCompile(`ls`)})
		call.Expect(t, eval.Args{"command": regexp.MustCompile(`/data`)})
	})

	r.Prompt(t, "sandbox-write", "Put the line hello into /data/hi.txt on the sandbox scratch.", func(t *testing.T, res *eval.Result) {
		call := res.Last(t)
		assert.Equal(t, "sandbox_write", call.Tool)
		call.Expect(t, eval.Args{"key": "scratch", "path": "/data/hi.txt", "content": regexp.MustCompile(`hello`)})
		assert.Subset(t, []string{"instance_list", "sandbox_write"}, res.Tools())
	})

	r.Prompt(t, "sandbox-read", "What does /etc/hostname contain on the sandbox scratch?", func(t *testing.T, res *eval.Result) {
		res.Call(t, "sandbox_read").Expect(t, eval.Args{"key": "scratch", "path": "/etc/hostname"})
		assert.Subset(t, []string{"instance_list", "sandbox_read"}, res.Tools())
	})
}
