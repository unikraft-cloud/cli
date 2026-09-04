// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestAuth(t *testing.T) {
	r := runner(t, true, []string{staging, stable})

	out := r.Run(t, []string{"unikraft", "login", "--check"})
	assert.Regexp(t, `authentication token found`, out)

	out = r.Run(t, []string{"unikraft", "profile", "list"})
	assert.Regexp(t, `true`, out)

	out = r.Run(t, []string{"unikraft", "metro", "list"})
	assert.Regexp(t, `https?://`, out)

	out = r.Run(t, []string{"unikraft", "logout"})
	assert.Regexp(t, `logout successful`, out)

	r.Run(t, []string{"unikraft", "profile", "list"}, integ.AllowFail())
	r.Run(t, []string{"unikraft", "metro", "list"}, integ.AllowFail())
}

// TestLoginProd exercises a real login flow against production using the
// token already configured in the local profile, starting from a brand new,
// hardcoded (empty) configuration file.
func TestLoginProd(t *testing.T) {
	integ.SkipUnlessIntegration(t)
	integ.SkipUnlessSupportedServer(t, []string{prod})
	t.Parallel()

	unikraftPath := integ.BuildUnikraft(t)

	baseCfg, err := integ.LoadConfig(t)
	require.NoError(t, err)
	if baseCfg == nil || baseCfg.Profile.Token == "" {
		t.Skip("test requires a configured profile with a token")
	}

	tokenPath := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte(baseCfg.Profile.Token), 0o600))

	// Start from a brand new, hardcoded config that doesn't exist yet.
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	partitionPath := filepath.Join(t.TempDir(), "partition.json")
	r := integ.NewTestEnv(t, unikraftPath).
		WithConfig(nil, configPath).
		WithPartitionPath(partitionPath)

	out := r.Run(t, []string{
		"unikraft", "login",
		"--token", tokenPath,
		"--organization", baseCfg.Profile.Organization,
	})
	require.Regexp(t, `login successful`, out)

	// HACK: these assertions are fragile, but `fra` is a known public metro

	out = r.Run(t, []string{"unikraft", "metro", "list"})
	require.NotEmpty(t, out)
	require.Regexp(t, `\bfra\b`, out)

	out = r.Run(t, []string{"unikraft", "metro", "get", "fra"})
	require.Regexp(t, `name:\s*fra`, out)
	require.Regexp(t, `location:\s*fra`, out)

	out = r.Run(t, []string{"unikraft", "api", "--metro=fra", "/v1/healthz"})
	require.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
}
