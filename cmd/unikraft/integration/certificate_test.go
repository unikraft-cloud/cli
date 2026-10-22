// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestCertificates(t *testing.T) {
	t.Run("inspect-missing", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		missing := "test-" + uniq()

		out := r.Run(t, []string{"unikraft", "certificate", "inspect", missing}, integ.ExpectFail())
		assert.Contains(t, out, "references not found: ["+missing+"]")
		assert.NotContains(t, out, "could not mirror certificate data")
	})

	t.Run("create", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		certNameA := uniq()
		certNameB := uniq()
		certA := integ.GenerateCert(t, "")
		certB := integ.GenerateCert(t, "")

		out := r.Run(t, []string{"unikraft", "certificate", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))

		out = r.Run(t, []string{"unikraft", "certificate", "create", "--set", "name=test-" + certNameA, "--set", "cn=" + certA.CN, "--set", "chain=" + certA.Chain, "--set", "pkey=" + certA.Key, "--set", "metro=" + r.MetroName})
		assert.Regexp(t, `name:\s+test-`, out)
		assert.Regexp(t, `state:\s+valid`, out)

		out = r.Run(t, []string{"unikraft", "certificate", "create", "--set", "name=test-" + certNameB, "--set", "cn=" + certB.CN, "--set", "chain=" + certB.Chain, "--set", "pkey=" + certB.Key, "--set", "metro=" + r.MetroName})
		assert.Regexp(t, `name:\s+test-`, out)
		assert.Regexp(t, `state:\s+valid`, out)

		out = r.Run(t, []string{"unikraft", "certificate", "list"})
		assert.Regexp(t, `test-.*valid`, out)

		out = r.Run(t, []string{"unikraft", "certificate", "inspect", "test-" + certNameA, "test-" + certNameB})
		assert.Regexp(t, `state:\s+valid`, out)
		assert.Regexp(t, `common-name:`, out)

		out = r.Run(t, []string{"unikraft", "certificate", "delete", "test-" + certNameA, "test-" + certNameB})
		assert.Regexp(t, `test-`, out)
	})

	t.Run("create-shortcut-files", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		certName := uniq()
		cert := integ.GenerateCert(t, "")
		dir := t.TempDir()
		chainPath := filepath.Join(dir, "chain.pem")
		keyPath := filepath.Join(dir, "key.pem")
		require.NoError(t, os.WriteFile(chainPath, []byte(cert.Chain), 0o600))
		require.NoError(t, os.WriteFile(keyPath, []byte(cert.Key), 0o600))

		out := r.Run(t, []string{
			"unikraft", "certificate", "create",
			"--set", "name=test-" + certName,
			"--set", "metro=" + r.MetroName,
			"--cn", cert.CN,
			"--chain", chainPath,
			"--pkey", keyPath,
		})
		assert.Regexp(t, `state:\s+valid`, out)

		out = r.Run(t, []string{"unikraft", "certificate", "inspect", "test-" + certName})
		assert.Regexp(t, `state:\s+valid`, out)

		r.Run(t, []string{"unikraft", "certificate", "delete", "test-" + certName})
	})

	t.Run("serve", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		certName := uniq()
		domainName := uniq()
		instName := uniq()

		// Use a stable external FQDN whose CN we know up front, so we can
		// generate the certificate before creating the instance and attach it
		// inline. DNS for this name will not resolve, but we will dial the load
		// balancer directly using the IP resolved from the internal FQDN.
		externalFQDN := domainName + ".example.com"

		// Upload the certificate to Unikraft Cloud.
		cert := integ.GenerateCert(t, externalFQDN)
		r.Run(t, []string{
			"unikraft", "certificate", "create",
			"--set", "name=test-" + certName,
			"--set", "cn=" + cert.CN,
			"--set", "chain=" + cert.Chain,
			"--set", "pkey=" + cert.Key,
			"--set", "metro=" + r.MetroName,
		})

		// Create an nginx instance with both the external FQDN (for the cert)
		// and a short-name domain (to get a resolvable internal address).
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + externalFQDN + ".,certificate=test-" + certName,
			"--set", "service.domains=name=" + domainName,
		})

		// Retrieve the internal FQDN (short-name domain) and resolve it to an IP
		// so we can dial the load balancer directly, bypassing DNS for example.com.
		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", `template={{ range .service.domains }}{{ if not (hasSuffix .fqdn "example.com") }}{{ .fqdn }}{{ end }}{{ end }}`,
		})
		internalFQDN := strings.TrimSpace(out)
		require.NotEmpty(t, internalFQDN, "expected a non-empty FQDN from the service domain")

		addrs, err := net.LookupHost(internalFQDN)
		require.NoError(t, err, "failed to resolve internal FQDN %s", internalFQDN)
		require.NotEmpty(t, addrs, "no addresses for internal FQDN %s", internalFQDN)
		dialAddr := net.JoinHostPort(addrs[0], "443")

		r.Run(t, []string{
			"unikraft", "instance", "wait",
			"--until", "state==running",
			"--timeout", "30s",
			"test-" + instName,
		})

		// Dial the LB IP directly with ServerName set to the external FQDN so
		// SNI selects the certificate we uploaded rather than the default one.
		tlsCerts := integ.HTTPGetTLSCerts(t, "https://"+externalFQDN, dialAddr)
		require.NotEmpty(t, tlsCerts, "TLS handshake returned no certificates")

		served := tlsCerts[0]
		expected := cert.X509Cert
		assert.Equal(t, expected.Subject.CommonName, served.Subject.CommonName,
			"served certificate CN should match uploaded certificate")
		assert.Equal(t, expected.SerialNumber, served.SerialNumber,
			"served certificate serial number should match uploaded certificate")
		assert.Equal(t, expected.RawSubjectPublicKeyInfo, served.RawSubjectPublicKeyInfo,
			"served certificate public key should match uploaded certificate")
		assert.Equal(t, expected.Raw, served.Raw,
			"served certificate should be byte-for-byte identical to the uploaded certificate")
	})
}
