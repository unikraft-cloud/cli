// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	integ "unikraft.com/cli/internal/integration"
)

func TestInstanceTemplates(t *testing.T) {
	t.Run("template", func(t *testing.T) {
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
			"unikraft", "instance", "template", "create", "test-" + instName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		out = r.Run(t, []string{"unikraft", "instance", "template", "list"})
		assert.Regexp(t, `NAME`, out)

		out = r.Run(t, []string{"unikraft", "instance", "template", "inspect", templateName})
		assert.Regexp(t, `state:\s+template`, out)
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.Regexp(t, `memory:\s+128`, out)

		r.Run(t, []string{"unikraft", "instance", "template", "edit", templateName, "--set", "tags=env-dev"})

		out = r.Run(t, []string{"unikraft", "instance", "template", "inspect", templateName})
		assert.Regexp(t, `state:\s+template`, out)

		r.Run(t, []string{"unikraft", "instance", "template", "delete", templateName})
	})

	t.Run("create-is-immediately-visible", func(t *testing.T) {
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

		// The create output itself must describe the template.
		out := r.Run(t, []string{
			"unikraft", "instance", "template", "create", "test-" + instName,
		})
		assert.NotContains(t, out, "references not found")
		assert.Regexp(t, `state:\s+template`, out)
		assert.Contains(t, out, "test-"+instName)

		r.Run(t, []string{"unikraft", "instance", "template", "delete", "test-" + instName})
	})

	t.Run("create-from-template", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-base-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "template", "create", "test-base-" + instName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-from-template-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "template=" + templateName,
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-from-template-" + instName})
		assert.Regexp(t, `state:\s+(stopping|stopped)\b`, out)
		assert.Regexp(t, `memory:\s+128`, out)

		r.Run(t, []string{"unikraft", "instance", "template", "delete", templateName})
	})

	// volumes proves the mountpoints survive the in-place conversion, and that
	// the attached volume becomes a volume template - so it is reachable via
	// `volume template` and no longer via `volume`.
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

		out := r.Run(t, []string{
			"unikraft", "instance", "template", "create", "test-" + instName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		out = r.Run(t, []string{"unikraft", "instance", "template", "inspect", templateName, "-f", "all"})
		assert.Contains(t, out, "test-"+volName)
		assert.Regexp(t, `at:\s+/data`, out)

		out = r.Run(t, []string{"unikraft", "volume", "template", "list", "--output", "quiet"})
		assert.Contains(t, out, "test-"+volName)

		out = r.Run(t, []string{"unikraft", "volume", "list", "--output", "quiet"})
		assert.NotContains(t, out, "test-"+volName)

		out = r.Run(t, []string{"unikraft", "volume", "template", "inspect", "test-" + volName})
		assert.Regexp(t, `state:\s+template`, out)
		r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + volName}, integ.ExpectFail())

		// Deleting the template takes its volume template with it: the
		// conversion re-parented the volume to the template.
		r.Run(t, []string{"unikraft", "instance", "template", "delete", templateName})
		r.Run(t, []string{"unikraft", "volume", "template", "delete", "test-" + volName}, integ.AllowFail())
	})

	t.Run("tags", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		instName := uniq()

		// Create a base instance for templating.
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
			"unikraft", "instance", "template", "create", "test-" + instName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		// Edit: set tags on template.
		r.Run(t, []string{
			"unikraft", "instance", "template", "edit", templateName,
			"--output", "quiet",
			"--set", "tags=env-prod",
			"--set", "tags=team-core",
		})
		out = r.Run(t, []string{"unikraft", "instance", "template", "inspect", templateName})
		assert.Regexp(t, `tags:.*env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)

		// Filter by tag.
		out = r.Run(t, []string{"unikraft", "instance", "template", "list", "--filter", "tags.*==env-prod"})
		assert.Contains(t, out, templateName)

		out = r.Run(t, []string{"unikraft", "instance", "template", "list", "--filter", "tags.*==no-match"})
		assert.NotContains(t, out, templateName)

		// Edit: add a tag.
		r.Run(t, []string{
			"unikraft", "instance", "template", "edit", templateName,
			"--output", "quiet",
			"--add", "tags=added-tag",
		})
		out = r.Run(t, []string{"unikraft", "instance", "template", "inspect", templateName})
		assert.Regexp(t, `tags:.*env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		// Edit: del a tag.
		r.Run(t, []string{
			"unikraft", "instance", "template", "edit", templateName,
			"--output", "quiet",
			"--del", "tags=env-prod",
		})
		out = r.Run(t, []string{"unikraft", "instance", "template", "inspect", templateName})
		assert.NotRegexp(t, `env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		r.Run(t, []string{"unikraft", "instance", "template", "delete", templateName})
	})
}
