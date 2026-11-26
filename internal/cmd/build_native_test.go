// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildCompressNeedsLocalOutput(t *testing.T) {
	t.Parallel()

	remote := "https://build.example.com"
	for name, tc := range map[string]struct {
		cmd  ImageBuildCmd
		want string
	}{
		"remote build": {ImageBuildCmd{Compress: true, Remote: &remote, Output: "my-org/my-app:latest"}, "--compress applies only to a local build"},
		"no output":    {ImageBuildCmd{Compress: true}, "--compress needs --output"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.EqualError(t, tc.cmd.Run(t.Context(), nil, nil), tc.want)
		})
	}
}
