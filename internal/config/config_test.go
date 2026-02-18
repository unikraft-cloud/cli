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

	require.NoError(t, os.WriteFile(path, []byte(input), 0o600))

	config := &Config{
		Path:           path,
		DefaultProfile: InterpolateString("default"),
		Profiles: map[string]Profile{
			"default": {
				Type:  InterpolateString(string(ProfileTypeCloud)),
				Token: InterpolateString("newtoken"),
			},
		},
	}

	require.NoError(t, config.Save())

	output, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(output)

	require.Contains(t, content, "# global comment", "expected global comment to be preserved")
	require.Contains(t, content, "# default profile comment", "expected profile comment to be preserved")
	require.Contains(t, content, "token: newtoken", "expected token to be updated")
	require.NotContains(t, content, "foobar", "expected unknown profile key to be removed")
}

func TestLoadInterpolatesTokenForValidationButKeepsRaw(t *testing.T) {
	t.Setenv("UKC_TOKEN", "s3cr3t")

	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: cloud
    token: ${UKC_TOKEN}
`) + "\n"

	require.NoError(t, os.WriteFile(path, []byte(input), 0o600))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Contains(t, cfg.Profiles, "default")
	profile := cfg.Profiles["default"]
	require.Equal(t, "${UKC_TOKEN}", profile.Token.Raw())
	require.Equal(t, "s3cr3t", profile.Token.String())
}

func TestLoadFailsValidationWhenInterpolatedTokenEmpty(t *testing.T) {
	t.Setenv("UKC_TOKEN", "")

	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: cloud
    token: ${UKC_TOKEN}
`) + "\n"

	require.NoError(t, os.WriteFile(path, []byte(input), 0o600))

	_, err := Load(path)
	require.Error(t, err)
}

func TestSavePreservesRawPlaceholders(t *testing.T) {
	t.Setenv("UKC_TOKEN", "s3cr3t")

	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	cfg := &Config{
		Path:           path,
		DefaultProfile: InterpolateString("default"),
		Profiles: map[string]Profile{
			"default": {
				Type:  InterpolateString(string(ProfileTypeCloud)),
				Token: InterpolateString("${UKC_TOKEN}"),
			},
		},
	}

	require.NoError(t, cfg.Save())

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(out)
	require.NotContains(t, content, "s3cr3t", "expected saved config not to contain expanded secret")
	require.Contains(t, content, "${UKC_TOKEN}", "expected saved config to preserve placeholder")
}

func TestSaveOmitsZeroInterpolateFields(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	cfg := &Config{
		Path:           path,
		DefaultProfile: InterpolateString("default"),
		Profiles: map[string]Profile{
			"default": {
				Type:  InterpolateString(string(ProfileTypeCloud)),
				Token: InterpolateString("token"),
			},
		},
	}

	require.NoError(t, cfg.Save())

	out, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(out)
	require.NotContains(t, content, "organization:", "expected organization to be omitted")
	require.NotContains(t, content, "controlplane:", "expected controlplane to be omitted")
	require.Contains(t, content, "token: token", "expected token to be present")
}
