// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
)

func TestConfig(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		out := r.Run(t, []string{"unikraft", "config", "get"})
		assert.Regexp(t, `profile:\s+\S+`, out)
		assert.Regexp(t, `token:`, out)

		out = r.Run(t, []string{"unikraft", "config", "get", "-o", "json"})
		assert.Regexp(t, `"token":`, out)
		assert.Regexp(t, `"profile":`, out)

		out = r.Run(t, []string{"unikraft", "config", "get", "-o", "yaml"})
		assert.Regexp(t, `token:`, out)
		assert.Regexp(t, `profile:`, out)
	})

	t.Run("get-explicit-file", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})
		profileName := "test-" + uniq()
		profile := *r.Profile
		profile.Name = profileName
		configPath := filepath.Join(t.TempDir(), "alternate.yaml")
		cfg := &config.Config{
			Path:           configPath,
			DefaultProfile: profileName,
			Profiles:       map[string]config.Profile{profileName: profile},
		}
		require.NoError(t, cfg.Save())

		out := r.Run(t, []string{"unikraft", "config", "get", configPath, "--output", "json"})
		var cfgs []struct {
			Profile string `json:"profile"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &cfgs))
		require.Len(t, cfgs, 1)
		assert.Equal(t, profileName, cfgs[0].Profile)
	})
}
