// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSavePreservesCommentsAndDropsUnknownKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	input := strings.TrimSpace(`
# global comment
profile: default
profiles:
  # default profile comment
  default:
    type: cloud
    token: oldtoken
    foobar: remove-me
`) + "\n"

	err := os.WriteFile(path, []byte(input), 0o600)
	require.NoError(t, err, "write config")

	config := &Config{
		Path:           path,
		DefaultProfile: "default",
		Profiles: map[string]Profile{
			"default": {
				Type:  ProfileTypeCloud,
				Token: "newtoken",
			},
		},
	}

	err = config.Save()
	require.NoError(t, err, "save config")

	output, err := os.ReadFile(path)
	require.NoError(t, err, "read config")
	content := string(output)

	assert.Contains(t, content, "# global comment", "expected global comment to be preserved")
	assert.Contains(t, content, "# default profile comment", "expected profile comment to be preserved")
	assert.Contains(t, content, "token: newtoken", "expected token to be updated")
	assert.NotContains(t, content, "foobar", "expected unknown profile key to be removed")
}

func TestLegacyProfileParsing(t *testing.T) {
	tests := []struct {
		name             string
		metroEnv         string
		expectedName     string
		expectedEndpoint string
	}{
		{
			name:             "full URL",
			metroEnv:         "https://api.fra.unikraft.cloud/v1",
			expectedName:     "fra",
			expectedEndpoint: "https://api.fra.unikraft.cloud",
		},
		{
			name:             "full URL with trailing slash",
			metroEnv:         "https://api.fra.unikraft.cloud/v1/",
			expectedName:     "fra",
			expectedEndpoint: "https://api.fra.unikraft.cloud",
		},
		{
			name:             "local proxy URL",
			metroEnv:         "http://localhost:8080/v1",
			expectedName:     "localhost:8080",
			expectedEndpoint: "http://localhost:8080",
		},
		{
			name:             "short name",
			metroEnv:         "fra",
			expectedName:     "fra",
			expectedEndpoint: "https://api.fra.unikraft.cloud",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("UKC_METRO", tt.metroEnv)
			t.Setenv("UKC_TOKEN", "test-token")

			root := t.TempDir()
			path := filepath.Join(root, "config.yaml")

			input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: legacy
`) + "\n"

			err := os.WriteFile(path, []byte(input), 0o600)
			require.NoError(t, err)

			config, err := Load(path)
			require.NoError(t, err)

			profile := config.Profiles["default"]
			assert.Equal(t, ProfileTypeLegacy, profile.Type)
			assert.Equal(t, "test-token", profile.Token)
			require.Len(t, profile.Metros, 1)

			metro := profile.Metros[0]
			assert.Equal(t, tt.expectedName, metro.Name)
			assert.Equal(t, tt.expectedEndpoint, metro.Endpoint)
		})
	}
}

func TestCloudProfileParsesMetroLocation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: cloud
    token: test-token
    metros:
      - name: fra
        endpoint: https://api.fra.unikraft.cloud
        location: fra
        insecure: true
`) + "\n"

	err := os.WriteFile(path, []byte(input), 0o600)
	require.NoError(t, err)

	config, err := Load(path)
	require.NoError(t, err)

	profile := config.Profiles["default"]
	require.Len(t, profile.Metros, 1)

	metro := profile.Metros[0]
	assert.Equal(t, "fra", metro.Name)
	assert.Equal(t, "fra", metro.Location, "location should be the parsed IATA code")
}

// TestCloudProfileMissingLocation ensures a metro without a location field
// loads without error and leaves Location empty.
func TestCloudProfileMissingLocation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: cloud
    token: test-token
    metros:
      - name: fra
        endpoint: https://api.fra.unikraft.cloud
`) + "\n"

	err := os.WriteFile(path, []byte(input), 0o600)
	require.NoError(t, err)

	config, err := Load(path)
	require.NoError(t, err)
	require.Len(t, config.Profiles["default"].Metros, 1)
	assert.Empty(t, config.Profiles["default"].Metros[0].Location, "missing location should default to empty")
}

// TestCloudProfileLegacyCountryIgnored ensures that a legacy `country:` key
// on a metro is silently ignored rather than breaking config loading.
func TestCloudProfileLegacyCountryIgnored(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: cloud
    token: test-token
    metros:
      - name: fra
        endpoint: https://api.fra.unikraft.cloud
        country: de
`) + "\n"

	err := os.WriteFile(path, []byte(input), 0o600)
	require.NoError(t, err)

	config, err := Load(path)
	require.NoError(t, err, "legacy country key should not break loading")
	require.Len(t, config.Profiles["default"].Metros, 1)

	metro := config.Profiles["default"].Metros[0]
	assert.Equal(t, "fra", metro.Name)
	assert.Empty(t, metro.Location, "legacy country should not populate location")
}

func TestLegacyProfileSaveDoesNotPersistEnvVars(t *testing.T) {
	t.Setenv("UKC_METRO", "https://api.fra.unikraft.cloud/v1")
	t.Setenv("UKC_TOKEN", "test-token-secret")

	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: legacy
`) + "\n"

	err := os.WriteFile(path, []byte(input), 0o600)
	require.NoError(t, err)

	config, err := Load(path)
	require.NoError(t, err)

	err = config.Save()
	require.NoError(t, err)

	output, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(output)

	assert.NotContains(t, content, "test-token-secret")
	assert.NotContains(t, content, "metros")
	assert.Contains(t, content, "type: legacy")
}

func TestEnvProfile(t *testing.T) {
	tests := []struct {
		name             string
		profileEnv       string
		metroEnv         string
		expectedName     string
		expectedEndpoint string
	}{
		{
			name:             "truthy with full URL",
			profileEnv:       "true",
			metroEnv:         "https://api.fra.unikraft.cloud/v1",
			expectedName:     "fra",
			expectedEndpoint: "https://api.fra.unikraft.cloud",
		},
		{
			name:             "truthy with local proxy URL",
			profileEnv:       "true",
			metroEnv:         "http://localhost:8080/v1",
			expectedName:     "localhost:8080",
			expectedEndpoint: "http://localhost:8080",
		},
		{
			name:             "truthy with short name",
			profileEnv:       "1",
			metroEnv:         "fra",
			expectedName:     "fra",
			expectedEndpoint: "https://api.fra.unikraft.cloud",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("UNIKRAFT_PROFILE_ENV", tt.profileEnv)
			t.Setenv("UKC_METRO", tt.metroEnv)
			t.Setenv("UKC_TOKEN", "env-token")

			// No config file is needed; the virtual profile bypasses it.
			config := &Config{}
			profile, err := config.CurrentProfile()
			require.NoError(t, err)

			assert.Equal(t, EnvProfile, profile.Name)
			assert.Equal(t, ProfileTypeLegacy, profile.Type)
			assert.Equal(t, "env-token", profile.Token)
			require.Len(t, profile.Metros, 1)
			assert.Equal(t, tt.expectedName, profile.Metros[0].Name)
			assert.Equal(t, tt.expectedEndpoint, profile.Metros[0].Endpoint)
		})
	}
}

func TestEnvProfileDisabled(t *testing.T) {
	// Unset / falsy values must not synthesize a virtual profile.
	for _, val := range []string{"", "false", "0", "no", "not-a-bool"} {
		name := val
		if name == "" {
			name = "unset"
		}

		t.Run(name, func(t *testing.T) {
			if val != "" {
				t.Setenv("UNIKRAFT_PROFILE_ENV", val)
			}
			t.Setenv("UKC_TOKEN", "env-token")

			config := &Config{}
			_, err := config.CurrentProfile()
			assert.ErrorIs(t, err, ErrNotSetup)
		})
	}
}

func TestEnvProfileBypassesConfigFile(t *testing.T) {
	// When enabled, the env-derived profile takes precedence over on-disk profiles.
	t.Setenv("UNIKRAFT_PROFILE_ENV", "true")
	t.Setenv("UKC_METRO", "fra")
	t.Setenv("UKC_TOKEN", "env-token")

	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: cloud
    token: on-disk-token
`) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(input), 0o600))

	config, err := Load(path)
	require.NoError(t, err)

	profile, err := config.CurrentProfile()
	require.NoError(t, err)
	assert.Equal(t, EnvProfile, profile.Name)
	assert.Equal(t, EnvProfile, config.CurrentProfileName())
	assert.Equal(t, "env-token", profile.Token)
	assert.Equal(t, ProfileTypeLegacy, profile.Type)
}

func TestProfile_GetDefaultMetro(t *testing.T) {
	tests := []struct {
		name     string
		profile  Profile
		expected string
	}{
		{
			name: "explicit default metro",
			profile: Profile{
				MetroDefault: "fra",
				Metros: []Metro{
					{Name: "lhr"},
					{Name: "fra"},
				},
			},
			expected: "fra",
		},
		{
			name: "single configured metro",
			profile: Profile{
				Metros: []Metro{
					{Name: "lhr"},
				},
			},
			expected: "lhr",
		},
		{
			name: "multiple metros without explicit default",
			profile: Profile{
				Metros: []Metro{
					{Name: "lhr"},
					{Name: "fra"},
				},
			},
			expected: "",
		},
		{
			name:     "no metros",
			profile:  Profile{},
			expected: "",
		},
		{
			name: "explicit default takes precedence over single metro",
			profile: Profile{
				MetroDefault: "fra",
				Metros: []Metro{
					{Name: "lhr"},
				},
			},
			expected: "fra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.profile.GetDefaultMetro()
			if got != tt.expected {
				t.Errorf("GetDefaultMetro() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCloudProfileMetroProxy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: cloud
    token: test-token
    metros:
      - name: fra
        endpoint: https://api.fra.unikraft.cloud
        proxy: https://console.example.com/proxy/fra
      - name: was
        endpoint: https://api.was.unikraft.cloud
`) + "\n"

	err := os.WriteFile(path, []byte(input), 0o600)
	require.NoError(t, err)

	config, err := Load(path)
	require.NoError(t, err)

	profile := config.Profiles["default"]
	require.Len(t, profile.Metros, 2)

	proxied := profile.Metros[0]
	assert.Equal(t, "https://api.fra.unikraft.cloud", proxied.Endpoint)
	assert.Equal(t, "https://console.example.com/proxy/fra", proxied.Proxy)
	assert.Equal(t, "https://console.example.com/proxy/fra", proxied.URL())
	assert.Equal(t, "index.fra.unikraft.cloud", proxied.Index().Host)

	direct := profile.Metros[1]
	assert.Empty(t, direct.Proxy)
	assert.Equal(t, "https://api.was.unikraft.cloud", direct.URL())
	assert.Equal(t, "index.was.unikraft.cloud", direct.Index().Host)
}
