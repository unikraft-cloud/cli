// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
)

func TestBuilderBuildURL(t *testing.T) {
	t.Parallel()

	list := func(buildURL, remote string) error {
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte("profile: a\nprofiles:\n  a: {type: cloud, token: a, build_url: "+buildURL+"}\n"), 0o600))
		cfg, err := loadConfig(path, "")
		require.NoError(t, err)
		ctx := context.WithValue(config.WithConfig(t.Context(), cfg), builderRemoteKey{}, remote)
		_, err = Builder{}.List(ctx)
		return err
	}

	require.ErrorContains(t, list("http://profile.invalid", ""), "the build service http://profile.invalid must be https")
	require.ErrorContains(t, list("http://profile.invalid", "http://flag.invalid"), "the build service http://flag.invalid must be https")
}
