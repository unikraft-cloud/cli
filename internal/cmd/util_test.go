// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
)

func TestRecoverCreatedFallsBackToCreateResponse(t *testing.T) {
	metro := config.Metro{Name: "fra", Endpoint: "https://api.fra.unikraft.cloud"}
	ctx := config.WithConfig(t.Context(), &config.Config{
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {
				Type:   config.ProfileTypeCloud,
				Name:   "default",
				Token:  "token",
				Metros: []config.Metro{metro},
			},
		},
	})

	known := multimetro.Key{Metro: "fra", Name: "linuxssh", UUID: "u1"}
	unknown := multimetro.Key{Metro: "fra", Name: "gone", UUID: "u2"}

	createdData := map[string]createdResource[platform.Instance]{
		known.String(): {
			data: platform.Instance{
				Uuid:  "u1",
				Name:  "linuxssh",
				State: platform.InstanceStateStopping,
			},
			metro: metro,
		},
	}

	refs := group.Refs{group.Ref(known), group.Ref(unknown)}
	recovered, missing := recoverCreated(ctx, refs, createdData, InstanceTemplate{}.load)

	require.Len(t, recovered, 1, "the created template should be recovered")
	tmpl, ok := recovered[0].(InstanceTemplate)
	require.True(t, ok, "recovered resource should be an InstanceTemplate")
	assert.Equal(t, "linuxssh", tmpl.Name)
	assert.Equal(t, "u1", tmpl.UUID)
	assert.Equal(t, known.String(), tmpl.Key().String())

	require.Len(t, missing, 1, "a ref with nothing to fall back on stays missing")
	assert.Equal(t, "gone", missing[0].Name)
}

// TestRecoverCreatedWithoutProfileReportsAllMissing ensures a broken config
// cannot silently turn missing resources into recovered ones.
func TestRecoverCreatedWithoutProfileReportsAllMissing(t *testing.T) {
	key := multimetro.Key{Metro: "fra", Name: "linuxssh", UUID: "u1"}
	createdData := map[string]createdResource[platform.Instance]{
		key.String(): {data: platform.Instance{Uuid: "u1", Name: "linuxssh"}},
	}

	recovered, missing := recoverCreated(t.Context(), group.Refs{group.Ref(key)}, createdData, InstanceTemplate{}.load)

	assert.Empty(t, recovered)
	require.Len(t, missing, 1)
	assert.Equal(t, "linuxssh", missing[0].Name)
}
