// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"

	"unikraft.com/cli/internal/eval"
	integ "unikraft.com/cli/internal/integration"
)

const liveImage = "nginx:latest"

func waitState(t *testing.T, env *integ.TestEnv, name, state string) {
	t.Helper()
	env.Run(t, []string{"unikraft", "--timeout", "60s", "instance", "wait", "--until", "state==" + state, name})
}

// newInstance creates an instance from a public image with the CLI and gives
// its name.
func newInstance(t *testing.T, env *integ.TestEnv, args ...string) string {
	t.Helper()
	name := "test-" + integ.Uniq()
	env.Run(t, append([]string{
		"unikraft", "instance", "create",
		"--output", "quiet",
		"--name", name,
		"--metro", env.Config.MetroName,
		"--image", liveImage,
		"--memory", "256",
	}, args...))
	return name
}

func TestLive(t *testing.T) {
	t.Run("sandbox-create", func(t *testing.T) {
		r, env := live(t)
		name := "test-" + integ.Uniq()
		r.Prompt(t, "sandbox-create", "Create a sandbox named "+name+" with a persistent /data directory and tell me when it is running.", func(t *testing.T, res *eval.Result) {
			call := res.Call(t, "instance_create")
			call.Expect(t, eval.Args{
				"set.name":    name,
				"set.plugins": regexp.MustCompile(`name=sandbox`),
				"set.volumes": regexp.MustCompile(`/data`),
			})
			assert.Subset(t, []string{"instance_create", "instance_list"}, res.Tools())
			assert.Len(t, res.Calls("instance_create"), 1)
			for _, step := range res.Steps {
				assert.False(t, step.IsError, "%s failed: %s", step.Tool, step.Result)
			}

			waitState(t, env, name, "running")
			out := env.Run(t, []string{"unikraft", "instance", "inspect", name, "--output", "json"})
			assert.Contains(t, gjson.Get(out, "0.plugins.#.name").Value(), "sandbox")
			assert.Contains(t, gjson.Get(out, "0.volumes.#.at").Value(), "/data")
		})
	})

	t.Run("sandbox-exec-live", func(t *testing.T) {
		r, _ := live(t)
		name := "test-" + integ.Uniq()
		r.Prompt(t, "sandbox-exec-live", "Create a sandbox named "+name+", wait until it is running, then run `echo ok` in it and tell me the output.", func(t *testing.T, res *eval.Result) {
			res.Call(t, "instance_create").Expect(t, eval.Args{"set.name": name, "set.plugins": regexp.MustCompile(`name=sandbox`)})
			res.Call(t, "sandbox_exec").Expect(t, eval.Args{"key": regexp.MustCompile(regexp.QuoteMeta(name)), "command": regexp.MustCompile(`echo ok`)})
			assert.Subset(t, []string{"instance_create", "instance_list", "sandbox_exec"}, res.Tools())
			assert.Len(t, res.Calls("instance_create"), 1)
			assert.Regexp(t, `\bok\b`, res.Text)
		})
	})

	t.Run("stop", func(t *testing.T) {
		r, env := live(t)
		name := newInstance(t, env, "--autostart")
		waitState(t, env, name, "running")
		r.Prompt(t, "stop", "Stop "+name+".", func(t *testing.T, res *eval.Result) {
			res.Call(t, "instance_control").Expect(t, eval.Args{"action": "stop", "keys": []string{name}})
			assert.Subset(t, []string{"instance_list", "instance_control"}, res.Tools())
			assert.Len(t, res.Calls("instance_control"), 1)
			waitState(t, env, name, "stopped")
		})
	})

	t.Run("memory-question", func(t *testing.T) {
		r, env := live(t)
		name := newInstance(t, env)
		r.Prompt(t, "memory-question", "How much memory does "+name+" have? Reply with the number of MiB only.", func(t *testing.T, res *eval.Result) {
			assert.Subset(t, []string{"instance_list"}, res.Tools())
			assert.Regexp(t, `\b256\b`, res.Text)
		})
	})
}
