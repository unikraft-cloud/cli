// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVolumeTemplates(t *testing.T) {
	t.Run("template", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "metro=" + r.MetroName,
			"--set", "size=10",
		})

		out := r.Run(t, []string{
			"unikraft", "volume", "template", "create", "test-" + volName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		out = r.Run(t, []string{"unikraft", "volume", "template", "list"})
		assert.Regexp(t, `NAME`, out)

		out = r.Run(t, []string{"unikraft", "volume", "template", "inspect", templateName})
		assert.Regexp(t, `state:\s+template`, out)
		assert.Regexp(t, `size:\s+10`, out)

		r.Run(t, []string{"unikraft", "volume", "template", "edit", templateName, "--set", "tags=env-dev"})

		out = r.Run(t, []string{"unikraft", "volume", "template", "inspect", templateName})
		assert.Regexp(t, `state:\s+template`, out)

		r.Run(t, []string{"unikraft", "volume", "template", "delete", templateName})
	})

	t.Run("create-from-template", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-base-" + volName,
			"--set", "metro=" + r.MetroName,
			"--set", "size=10",
		})

		out := r.Run(t, []string{
			"unikraft", "volume", "template", "create", "test-base-" + volName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		out = r.Run(t, []string{
			"unikraft", "volume", "create",
			"--set", "name=test-from-template-" + volName,
			"--set", "metro=" + r.MetroName,
			"--template", templateName,
		})
		assert.Regexp(t, `state:\s+(available|initializing)`, out)
		assert.Regexp(t, `size:\s+10`, out)

		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-from-template-" + volName})
		assert.Regexp(t, `state:\s+(available|initializing)`, out)
		assert.Regexp(t, `size:\s+10`, out)

		r.Run(t, []string{"unikraft", "volume", "template", "delete", templateName})
	})

	t.Run("tags", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()

		// Create a base volume for templating.
		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "metro=" + r.MetroName,
			"--set", "size=10",
		})

		out := r.Run(t, []string{
			"unikraft", "volume", "template", "create", "test-" + volName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		// Edit: set tags on template.
		r.Run(t, []string{
			"unikraft", "volume", "template", "edit", templateName,
			"--output", "quiet",
			"--set", "tags=env-prod",
			"--set", "tags=team-core",
		})
		out = r.Run(t, []string{"unikraft", "volume", "template", "inspect", templateName})
		assert.Regexp(t, `tags:.*env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)

		// Filter by tag.
		out = r.Run(t, []string{"unikraft", "volume", "template", "list", "--filter", "tags.*==env-prod"})
		assert.Contains(t, out, templateName)

		out = r.Run(t, []string{"unikraft", "volume", "template", "list", "--filter", "tags.*==no-match"})
		assert.NotContains(t, out, templateName)

		// Edit: add a tag.
		r.Run(t, []string{
			"unikraft", "volume", "template", "edit", templateName,
			"--output", "quiet",
			"--add", "tags=added-tag",
		})
		out = r.Run(t, []string{"unikraft", "volume", "template", "inspect", templateName})
		assert.Regexp(t, `tags:.*env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		// Edit: del a tag.
		r.Run(t, []string{
			"unikraft", "volume", "template", "edit", templateName,
			"--output", "quiet",
			"--del", "tags=env-prod",
		})
		out = r.Run(t, []string{"unikraft", "volume", "template", "inspect", templateName})
		assert.NotRegexp(t, `env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		r.Run(t, []string{"unikraft", "volume", "template", "delete", templateName})
	})
}
