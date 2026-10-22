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

func TestResources(t *testing.T) {
	t.Run("volume-flow", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()

		out := r.Run(t, []string{"unikraft", "resource", "create", "--set", "type=volume", "--set", "name=test-" + volName, "--set", "size=10", "--set", "metro=" + r.MetroName})
		assert.Regexp(t, `state:\s+(available|initializing)`, out)

		out = r.Run(t, []string{"unikraft", "resource", "get", "volume:" + r.MetroName + "/test-" + volName})
		assert.Regexp(t, `size:\s+10`, out)

		out = r.Run(t, []string{"unikraft", "resource", "list"})
		assert.Regexp(t, `volume`, out)

		r.Run(t, []string{"unikraft", "resource", "edit", "volume:" + r.MetroName + "/test-" + volName, "--set", "size=20"})

		out = r.Run(t, []string{"unikraft", "resource", "get", "volume:" + r.MetroName + "/test-" + volName})
		assert.Regexp(t, `size:\s+20`, out)

		out = r.Run(t, []string{"unikraft", "volume", "get", "test-" + volName})
		assert.Regexp(t, `size:\s+20`, out)

		r.Run(t, []string{"unikraft", "resource", "delete", "--all", "--force"})
		out = r.Run(t, []string{"unikraft", "volume", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))
	})

	t.Run("multi-type-flow", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		svcName := uniq()
		instName := uniq()
		domainName := uniq()
		svcKey := "service:" + r.MetroName + "/test-" + svcName
		instKey := "instance:" + r.MetroName + "/test-" + instName

		r.Run(t, []string{
			"unikraft", "resource", "create",
			"--set", "type=service",
			"--set", "name=test-" + svcName,
			"--set", "metro=" + r.MetroName,
			"--set", "services=443:8080/tls+http",
		})
		r.Run(t, []string{
			"unikraft", "resource", "create",
			"--set", "type=instance",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{"unikraft", "resource", "get", svcKey, instKey})
		assert.Contains(t, out, "test-"+svcName)
		assert.Contains(t, out, "test-"+instName)

		r.Run(t, []string{
			"unikraft", "resource", "edit", svcKey,
			"--add", "domains=fqdn=" + domainName + ".unikraft.example",
		})
		out = r.Run(t, []string{"unikraft", "resource", "get", svcKey})
		assert.Contains(t, out, domainName+".unikraft.example")

		r.Run(t, []string{"unikraft", "resource", "delete", svcKey, instKey})
		r.Run(t, []string{"unikraft", "service", "inspect", "test-" + svcName}, integ.ExpectFail())
		r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName}, integ.ExpectFail())
	})
}
