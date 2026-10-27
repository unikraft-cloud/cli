// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestFilterRunning(t *testing.T) {
	env, c := live(t)
	running := newInstance(t, env, "--autostart")
	stopped := newInstance(t, env)
	waitState(t, env, running, "running")

	reply := c.Send("Show the instances that are running in " + env.MetroName + ".")
	list := reply.Called("instance_list")
	require.NotEmpty(t, list)
	filter := strings.Join(decode[keysArgs](t, list[0].Args).Filter, " ")
	assert.Contains(t, filter, "state==running")
	assert.Contains(t, filter, "metro=="+env.MetroName)
	var names []string
	for _, r := range decode[listResult](t, list[0].Result).Resources {
		names = append(names, r.Name)
	}
	assert.Contains(t, names, running)
	assert.NotContains(t, names, stopped)
}

func TestLogs(t *testing.T) {
	env, c := live(t)
	name := newInstance(t, env, "--autostart")

	reply := c.Send("Show me the logs of " + name + ".")
	assert.Equal(t, []string{"instance_logs"}, reply.Tools())
	args := decode[struct {
		Key string `json:"key"`
	}](t, only(t, reply, "instance_logs").Args)
	assert.Equal(t, []string{name}, bare(args.Key))
}

func TestMemoryQuestion(t *testing.T) {
	env, c := live(t)
	name := newInstance(t, env)

	reply := c.Send("How much memory does " + name + " have? Reply with the number of MiB only.")
	require.NotEmpty(t, reply.Called("instance_list"))
	assert.Subset(t, []string{"instance_list"}, reply.Tools())
	assert.Regexp(t, `\b256\b`, reply.Text)
}

func TestStop(t *testing.T) {
	env, c := live(t)
	name := newInstance(t, env, "--autostart")
	waitState(t, env, name, "running")

	reply := c.Send("Stop " + name + ".")
	assert.Subset(t, []string{"instance_list", "instance_control"}, reply.Tools())
	args := decode[controlArgs](t, only(t, reply, "instance_control").Args)
	assert.Equal(t, "stop", args.Action)
	assert.Equal(t, []string{name}, bare(args.Keys...))
	waitState(t, env, name, "stopped")
}

func TestRestartBoth(t *testing.T) {
	env, c := live(t)
	first, second := newInstance(t, env, "--autostart"), newInstance(t, env, "--autostart")
	waitState(t, env, first, "running")
	waitState(t, env, second, "running")

	reply := c.Send("Restart " + first + " and " + second + ".")
	args := decode[controlArgs](t, only(t, reply, "instance_control").Args)
	assert.Equal(t, "restart", args.Action)
	assert.ElementsMatch(t, []string{first, second}, bare(args.Keys...))
}

func TestSuspend(t *testing.T) {
	env, c := live(t)
	name := newInstance(t, env, "--autostart")
	waitState(t, env, name, "running")

	reply := c.Send("Suspend " + name + " so it resumes on its next request.")
	control := reply.Called("instance_control")
	require.NotEmpty(t, control)
	args := decode[controlArgs](t, control[0].Args)
	assert.Equal(t, "suspend", args.Action)
	assert.Equal(t, []string{name}, bare(args.Keys...))
}

func TestDelete(t *testing.T) {
	env, c := live(t)
	name := newInstance(t, env)

	reply := c.Send("Remove " + name + " entirely, I no longer need it.")
	assert.NotContains(t, reply.Tools(), "instance_control")
	call := only(t, reply, "instance_delete")
	assert.Equal(t, []string{name}, bare(decode[keysArgs](t, call.Args).Keys...))
	assert.False(t, call.IsError, call.Text)
}

func TestCreateStopped(t *testing.T) {
	env, c := live(t)
	name := "test-" + integ.Uniq()

	reply := c.Send("Create an instance named " + name + " from " + liveImage + " but don't start it.")
	args := decode[patchArgs](t, only(t, reply, "instance_create").Args)
	assert.Equal(t, name, text(args.Set["name"]))
	assert.Equal(t, liveImage, text(args.Set["image"]))
	assert.Equal(t, "false", text(args.Set["autostart"]))
	waitState(t, env, name, "stopped")
}

func TestCreateStarted(t *testing.T) {
	env, c := live(t)
	name := "test-" + integ.Uniq()

	reply := c.Send("Create an instance named " + name + " from " + liveImage + ".")
	args := decode[patchArgs](t, only(t, reply, "instance_create").Args)
	assert.Equal(t, name, text(args.Set["name"]))
	assert.Equal(t, liveImage, text(args.Set["image"]))
	assert.NotEqual(t, "false", text(args.Set["autostart"]), "the instance was left stopped")
	waitState(t, env, name, "running")
}

func TestEditMemory(t *testing.T) {
	env, c := live(t)
	name := newInstance(t, env)

	reply := c.Send("Give " + name + " 1 GiB of memory.")
	call := only(t, reply, "instance_edit")
	args := decode[patchArgs](t, call.Args)
	assert.Equal(t, []string{name}, bare(args.Key))
	assert.Regexp(t, `^(1024|1Gi|1GiB|1G)$`, text(args.Set["resources.memory"]))
	assert.False(t, call.IsError, call.Text)
}

func TestDryRun(t *testing.T) {
	env, c := live(t)
	name := newInstance(t, env)

	reply := c.Send("Preview the patch for setting " + name + " to 2 vCPUs without applying it.")
	args := decode[patchArgs](t, only(t, reply, "instance_edit").Args)
	assert.Equal(t, []string{name}, bare(args.Key))
	assert.True(t, args.DryRun)
	assert.Equal(t, "2", text(args.Set["resources.vcpus"]))
}

func TestVolumeCreate(t *testing.T) {
	env, c := live(t)
	name := "test-" + integ.Uniq()

	reply := c.Send("Create a 2 GiB volume named " + name + " in " + env.MetroName + ".")
	call := only(t, reply, "volume_create")
	args := decode[patchArgs](t, call.Args)
	assert.Equal(t, name, text(args.Set["name"]))
	assert.Equal(t, env.MetroName, text(args.Set["metro"]))
	assert.Regexp(t, `^(2Gi|2GiB|2G|2048)$`, text(args.Set["size"]))
	assert.False(t, call.IsError, call.Text)
}

func TestImageGet(t *testing.T) {
	_, c := live(t)

	reply := c.Send("What does the image " + liveImage + " contain?")
	assert.Equal(t, []string{"image_get"}, reply.Tools())
	call := only(t, reply, "image_get")
	assert.Equal(t, []string{liveImage}, decode[keysArgs](t, call.Args).Keys)
	assert.False(t, call.IsError, call.Text)
}

func TestMetros(t *testing.T) {
	_, c := live(t)

	reply := c.Send("Which metros can I deploy to?")
	require.NotEmpty(t, reply.Calls)
	assert.Equal(t, "metro_list", reply.Calls[0].Name)
}

func TestSandbox(t *testing.T) {
	env, c := live(t)
	name := "test-" + integ.Uniq()
	cat := func() string {
		return env.Run(t, []string{"unikraft", "instance", "exec", name, "--", "cat", "/data/hi.txt"})
	}

	reply := c.Send("Create a sandbox named " + name + " with a persistent /data directory and tell me when it is running.")
	assert.Subset(t, []string{"instance_create", "instance_list"}, reply.Tools())
	for _, call := range reply.Calls {
		assert.False(t, call.IsError, "%s failed: %s", call.Name, call.Text)
	}
	args := decode[patchArgs](t, only(t, reply, "instance_create").Args)
	assert.Equal(t, name, text(args.Set["name"]))
	assert.Regexp(t, `official/(alpine|debian-slim)`, text(args.Set["image"]))
	plugins, volumes := string(args.Set["plugins"]), string(args.Set["volumes"])
	assert.Contains(t, plugins, "name=sandbox")
	assert.Contains(t, plugins, "plugins/sandbox")
	assert.Contains(t, plugins, "persist_path")
	assert.Contains(t, volumes, "/data")
	assert.Contains(t, volumes, "size=")
	waitState(t, env, name, "running")

	reply = c.Send("Write the line hello to /data/hi.txt in it.")
	write := decode[struct {
		Key     string `json:"key"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}](t, only(t, reply, "sandbox_write").Args)
	assert.Equal(t, []string{name}, bare(write.Key))
	assert.Equal(t, "/data/hi.txt", write.Path)
	assert.Contains(t, cat(), "hello")

	reply = c.Send("Use sed to replace hello with bye in that file.")
	exec := reply.Called("sandbox_exec")
	require.NotEmpty(t, exec)
	assert.Contains(t, string(exec[0].Args), "sed")
	assert.Contains(t, cat(), "bye")

	reply = c.Send("What does /data/hi.txt contain now? Reply with its content only.")
	read := reply.Called("sandbox_read")
	require.NotEmpty(t, read)
	assert.Contains(t, string(read[0].Args), "/data/hi.txt")
	assert.Contains(t, reply.Text, "bye")
}
