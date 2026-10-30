// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestBuilder(t *testing.T) {
	buildURL := os.Getenv("UNIKRAFT_X_TEST_BUILD_URL")
	if buildURL == "" {
		t.Skip("builder test requires UNIKRAFT_X_TEST_BUILD_URL")
	}
	r := runner(t, true, []string{staging, stable})

	// The build service's certificate may be self-signed.
	certFile := filepath.Join(t.TempDir(), "build-service.pem")
	cert := integ.HTTPGetTLSCerts(t, buildURL, "")[0]
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o644))
	env := integ.WithEnv("SSL_CERT_FILE="+certFile, "UNIKRAFT_BUILD_URL="+buildURL)

	// The account's remote builds share the builder, so it is not deleted.
	out := r.Run(t, []string{"unikraft", "builder", "create"}, env)
	assert.Regexp(t, `builder ready`, out)

	out = r.Run(t, []string{"unikraft", "builder", "du"}, env)
	assert.Regexp(t, `(?m)^Reclaimable: +\S+\nTotal: +\S+$`, out)

	out = r.Run(t, []string{"unikraft", "builder", "prune"}, env)
	assert.Regexp(t, `pruned the builder's cache freed=\S+`, out)
}
