// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServices(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		svcNameA := uniq()
		svcNameB := uniq()
		domainA := uniq()
		domainB := uniq()

		out := r.Run(t, []string{"unikraft", "service", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))

		out = r.Run(t, []string{
			"unikraft", "service", "create",
			"--set", "name=test-" + svcNameA,
			"--set", "metro=" + r.MetroName,
			"--set", "domains=fqdn=" + domainA + ".unikraft.example",
			"--set", "services=443:8080/tls+http",
			"--set", "services=80:443/http+redirect",
		})
		assert.Regexp(t, `name:\s+test-`, out)
		assert.Regexp(t, `source:\s+443`, out)
		assert.Regexp(t, `destination:\s+8080`, out)
		assert.Regexp(t, `source:\s+80`, out)
		assert.Regexp(t, `destination:\s+443`, out)
		assert.Regexp(t, `fqdn:`, out)

		// Same shape as above, via shortcut flags rather than --set entries.
		out = r.Run(t, []string{
			"unikraft", "service", "create",
			"--name", "test-" + svcNameB,
			"--metro", r.MetroName,
			"--domain", domainB + ".unikraft.example",
			"--service", "443:8080/tls+http",
			"--service", "80:443/http+redirect",
		})
		assert.Regexp(t, `name:\s+test-`, out)
		assert.Regexp(t, `source:\s+443`, out)
		assert.Regexp(t, `destination:\s+8080`, out)
		assert.Regexp(t, `source:\s+80`, out)
		assert.Regexp(t, `destination:\s+443`, out)
		assert.Regexp(t, `fqdn:`, out)

		out = r.Run(t, []string{"unikraft", "service", "list"})
		assert.Regexp(t, `test-`, out)

		out = r.Run(t, []string{"unikraft", "service", "inspect", "test-" + svcNameA, "test-" + svcNameB})
		assert.Regexp(t, `source:\s+443`, out)
		assert.Regexp(t, `destination:\s+8080`, out)
		assert.Regexp(t, `source:\s+80`, out)
		assert.Regexp(t, `destination:\s+443`, out)
		assert.Regexp(t, `fqdn:`, out)

		out = r.Run(t, []string{"unikraft", "service", "delete", "test-" + svcNameA, "test-" + svcNameB})
		assert.Regexp(t, `test-`, out)
	})

	t.Run("edit", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		svcName := uniq()
		domainName := uniq()
		domainEdit := uniq()

		r.Run(t, []string{
			"unikraft", "service", "create",
			"--output", "quiet",
			"--set", "name=test-" + svcName,
			"--set", "metro=" + r.MetroName,
			"--set", "domains=fqdn=" + domainName + ".unikraft.example",
			"--set", "services=443:8080/tls+http",
		})

		r.Run(t, []string{
			"unikraft", "service", "edit", "test-" + svcName,
			"--output", "quiet",
			"--set", "limits.soft=2",
			"--set", "limits.hard=10",
			"--set", "domains=fqdn=" + domainEdit + ".unikraft.example",
			"--set", "services=1000:2000/tls",
		})

		out := r.Run(t, []string{"unikraft", "service", "inspect", "test-" + svcName})
		assert.Regexp(t, `soft:\s+2`, out)
		assert.Regexp(t, `hard:\s+10`, out)
		assert.Regexp(t, `source:\s+1000`, out)
		assert.Regexp(t, `destination:\s+2000`, out)
		assert.Regexp(t, `fqdn:`, out)

		r.Run(t, []string{"unikraft", "service", "delete", "test-" + svcName})
	})

	t.Run("shortcuts-and-patches", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		svcName := uniq()
		domainName := uniq()
		addedDomainName := uniq()

		r.Run(t, []string{
			"unikraft", "service", "create",
			"--output", "quiet",
			"--name", "test-" + svcName,
			"--metro", r.MetroName,
			"--domain", domainName + ".unikraft.example",
			"--service", "443:8080/tls+http",
		})

		r.Run(t, []string{
			"unikraft", "service", "edit", "test-" + svcName,
			"--output", "quiet",
			"--add", "domains=fqdn=" + addedDomainName + ".unikraft.example",
			"--add", "services=8443:8080/tls",
		})
		out := r.Run(t, []string{"unikraft", "service", "inspect", "test-" + svcName})
		assert.Contains(t, out, domainName+".unikraft.example")
		assert.Contains(t, out, addedDomainName+".unikraft.example")
		assert.Regexp(t, `source:\s+8443`, out)

		r.Run(t, []string{
			"unikraft", "service", "edit", "test-" + svcName,
			"--output", "quiet",
			"--del", "domains=fqdn=" + addedDomainName + ".unikraft.example",
			"--del", "services=8443:8080/tls",
		})
		out = r.Run(t, []string{"unikraft", "service", "inspect", "test-" + svcName})
		assert.NotContains(t, out, addedDomainName+".unikraft.example")
		assert.NotRegexp(t, `source:\s+8443`, out)

		r.Run(t, []string{"unikraft", "service", "delete", "test-" + svcName})
	})
}
