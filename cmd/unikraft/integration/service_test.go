// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	integ "unikraft.com/cli/internal/integration"
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

	t.Run("udp", func(t *testing.T) {
		r := runner(t, true, []string{staging})

		out := r.Run(t, []string{
			"unikraft", "service", "create",
			"--metro", r.MetroName,
			"--service", "53:53/udp",
		}, integ.ExpectFail())
		assert.Contains(t, out, "UDP services must have an IP address configured")
	})

	t.Run("udp-ip", func(t *testing.T) {
		ip := os.Getenv("UNIKRAFT_X_TEST_UDP_IP")
		if ip == "" {
			t.Skip("udp test requires UNIKRAFT_X_TEST_UDP_IP")
		}
		r := runner(t, true, []string{staging})
		svcName := "test-" + uniq()
		port := 20000 + rand.IntN(40000)
		udpSvc := fmt.Sprintf("%d:%d/udp@%s", port, port, ip)
		tcpSvc := fmt.Sprintf("%d:8080/tls@%s", port, ip)
		udpOut := fmt.Sprintf(`source:\s+%d\s+destination:\s+%d\s+protocol:\s+udp\s+ip:\s+%s`, port, port, regexp.QuoteMeta(ip))
		tcpOut := fmt.Sprintf(`source:\s+%d\s+destination:\s+8080\s+protocol:\s+tcp\s+ip:\s+%s`, port, regexp.QuoteMeta(ip))

		out := r.Run(t, []string{
			"unikraft", "service", "create",
			"--name", svcName,
			"--metro", r.MetroName,
			"--service", udpSvc,
		})
		assert.Regexp(t, udpOut, out)

		r.Run(t, []string{
			"unikraft", "service", "edit", svcName,
			"--output", "quiet",
			"--add", "services=" + tcpSvc,
		})
		out = r.Run(t, []string{"unikraft", "service", "inspect", svcName})
		assert.Regexp(t, udpOut, out)
		assert.Regexp(t, tcpOut, out)

		r.Run(t, []string{
			"unikraft", "service", "edit", svcName,
			"--output", "quiet",
			"--del", "services=" + udpSvc,
		})
		out = r.Run(t, []string{"unikraft", "service", "inspect", svcName})
		assert.NotRegexp(t, udpOut, out)
		assert.Regexp(t, tcpOut, out)

		r.Run(t, []string{"unikraft", "service", "delete", svcName})
	})

	t.Run("delete-drain", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		svcName := "test-" + uniq()
		instName := "test-" + uniq()

		r.Run(t, []string{
			"unikraft", "service", "create",
			"--output", "quiet",
			"--name", svcName,
			"--metro", r.MetroName,
			"--service", "443:8080/tls+http",
		})
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--name", instName,
			"--metro", r.MetroName,
			"--image", "nginx:latest",
			"--service", svcName,
			"--autostart",
		})

		out := r.Run(t, []string{"unikraft", "service", "delete", "--all", "--force", "--drain"}, integ.ExpectFail())
		assert.Contains(t, out, "cannot be used with --all or --filter")
		out = r.Run(t, []string{"unikraft", "service", "delete", svcName, "--drain", "--drain-timeout=-1s"}, integ.ExpectFail())
		assert.Contains(t, out, "--drain-timeout cannot be negative")

		r.Run(t, []string{"unikraft", "--timeout", "120s", "service", "delete", svcName, "--drain"})

		r.Run(t, []string{"unikraft", "instance", "inspect", instName}, integ.WithNoPartition(), integ.ExpectFail())
		r.Run(t, []string{"unikraft", "service", "inspect", svcName}, integ.WithNoPartition(), integ.ExpectFail())
	})

	t.Run("delete-wait-empty", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		svcName := "test-" + uniq()
		instName := "test-" + uniq()

		r.Run(t, []string{
			"unikraft", "service", "create",
			"--output", "quiet",
			"--name", svcName,
			"--metro", r.MetroName,
			"--service", "443:8080/tls+http",
		})
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--name", instName,
			"--metro", r.MetroName,
			"--image", "nginx:latest",
			"--service", svcName,
		})

		r.Run(t, []string{"unikraft", "--timeout", "5s", "service", "delete", svcName, "--wait-empty"}, integ.ExpectFail())
		r.Run(t, []string{"unikraft", "instance", "inspect", instName})
		r.Run(t, []string{"unikraft", "service", "inspect", svcName})

		r.Run(t, []string{"unikraft", "instance", "delete", instName})
		r.Run(t, []string{"unikraft", "--timeout", "60s", "service", "delete", svcName, "--wait-empty"})
	})
}
