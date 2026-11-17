// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package login

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
)

func TestLoginRegistry(t *testing.T) {
	tests := []struct {
		name     string
		profiles map[string]config.Profile
		registry string
		expected string
	}{
		{
			name:     "stores the registry",
			registry: "harbor.example:8083",
			expected: "harbor.example:8083",
		},
		{
			name: "control plane change clears a stale host",
			profiles: map[string]config.Profile{
				"acme": {
					Type:         config.ProfileTypeCloud,
					ControlPlane: "https://old.example",
					Registry:     config.Registry{Host: "old.host"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/auth" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "success",
					"data": map[string]any{
						"organization_name": "acme",
						"registry":          tt.registry,
					},
				})
			}))
			defer srv.Close()

			dir := t.TempDir()
			tokenPath := filepath.Join(dir, "token")
			require.NoError(t, os.WriteFile(tokenPath, []byte("test-token"), 0o600))
			token, err := os.Open(tokenPath)
			require.NoError(t, err)

			path := filepath.Join(dir, "config.yaml")
			cmd := &LoginCmd{ControlPlane: srv.URL, Token: token}
			err = cmd.Run(t.Context(), &config.Config{Path: path, Profiles: tt.profiles})
			require.NoError(t, err)

			cfg, err := config.Load(path)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.Profiles["acme"].Registry.Host)
		})
	}
}
