// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestAPI(t *testing.T) {
	t.Run("healthz", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		out := r.Run(t, []string{"unikraft", "api", "--metro=" + r.MetroName, "/v1/healthz"})
		// Response should be valid JSON, pretty-printed (multi-line).
		assert.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
		assert.Contains(t, out, "\n", "expected pretty-printed (multi-line) JSON, got: %s", out)
	})

	t.Run("quotas", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		out := r.Run(t, []string{"unikraft", "api", "--metro=" + r.MetroName, "/v1/users/quotas"})
		require.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
		// Quotas responses include a "data" envelope.
		assert.Contains(t, out, `"data"`)
	})

	t.Run("instances list", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		out := r.Run(t, []string{"unikraft", "api", "--metro=" + r.MetroName, "/v1/instances"})
		assert.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
	})

	t.Run("full url with insecure", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		endpoint := strings.TrimRight(r.Metro.Endpoint, "/") + "/v1/healthz"
		out := r.Run(t, []string{"unikraft", "api", "-k", endpoint})
		assert.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
	})

	t.Run("volume from data file", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		volName := uniq()
		payload, err := json.Marshal(map[string]any{
			"name":    "test-" + volName,
			"size_mb": 10,
		})
		require.NoError(t, err)
		payloadPath := filepath.Join(t.TempDir(), "volume.json")
		require.NoError(t, os.WriteFile(payloadPath, payload, 0o600))
		deletePayload, err := json.Marshal([]map[string]string{{"name": "test-" + volName}})
		require.NoError(t, err)
		deleteArgs := []string{
			"unikraft", "api",
			"--metro=" + r.MetroName,
			"/v1/volumes",
			"--method", "DELETE",
			string(deletePayload),
		}

		out := r.Run(t, []string{
			"unikraft", "api",
			"--metro=" + r.MetroName,
			"/v1/volumes",
			"@" + payloadPath,
		})
		t.Cleanup(func() {
			out, err := r.RunRaw(t, deleteArgs, integ.WithoutCancel())
			assert.NoError(t, err, "deleting raw API-created volume: %s", out)
		})
		require.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
		assert.Contains(t, out, "test-"+volName)
	})

	t.Run("missing endpoint fails", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		out := r.Run(t, []string{"unikraft", "api", "--metro=" + r.MetroName, "/v1/this-endpoint-does-not-exist"}, integ.ExpectFail())
		assert.Regexp(t, `HTTP 4\d\d`, out)
	})
}
