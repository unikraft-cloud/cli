// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cli

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/controlplane"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/pkg/mcpserver"
	"unikraft.com/cli/pkg/mcpserver/mcptest"
)

func TestNew(t *testing.T) {
	srv, err := New(t.Context(), mcpserver.Flags{})
	require.NoError(t, err)
	names := mcptest.ConnectServer(t, t.Context(), srv.MCP()).ToolNames()
	assert.Contains(t, names, "instance_list")
	assert.NotContains(t, names, "profile_use")

	t.Setenv("UNIKRAFT_PROFILE_ENV", "true")
	_, err = New(t.Context(), mcpserver.Flags{})
	assert.ErrorContains(t, err, "UNIKRAFT_PROFILE_ENV")
}

func TestNewServerFiles(t *testing.T) {
	srv, err := New(t.Context(), mcpserver.Flags{})
	require.NoError(t, err)
	s := mcptest.ConnectServer(t, WithToken(t.Context(), "secret"), srv.MCP())
	rom, err := json.Marshal("name=r,dir=" + t.TempDir() + ",at=/r")
	require.NoError(t, err)

	for _, tc := range []struct{ tool, args, want string }{
		{"instance_create", `{"set":{"image":"nginx:latest","metro":"fra","roms":[` + string(rom) + `]}}`, "does not send its own files"},
		{"image_get", `{"keys":["http+oci://127.0.0.1:1/x:latest"]}`, "only registry references"},
	} {
		out, isErr, err := s.Call(t.Context(), tc.tool, json.RawMessage(tc.args))
		require.NoError(t, err, tc.tool)
		assert.True(t, isErr, tc.tool)
		assert.Contains(t, out, tc.want, tc.tool)
	}
}

func TestWithToken(t *testing.T) {
	profile, err := config.G(WithToken(t.Context(), "secret")).CurrentProfile()
	require.NoError(t, err)
	assert.Equal(t, config.ProfileTypeCloud, profile.Type)
	assert.Equal(t, "secret", profile.Token)
	assert.Equal(t, controlplane.DefaultEndpoint, profile.ControlPlane)
}
