// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/mirror"
)

func TestVolumeMountedByMirror(t *testing.T) {
	result := cmd.Volume{Volume: platform.Volume{
		Name:  "my-volume",
		State: platform.VolumeStateMounted,
		MountedBy: []platform.VolumeInstanceMount{
			{Name: "ro-inst", Readonly: true},
			{Name: "rw-inst"},
		},
	}}

	require.NoError(t, mirror.Mirror(result, &result))
	require.Len(t, result.MountedBy, 2)
	assert.Equal(t, "ro-inst", result.MountedBy[0].Name)
	assert.True(t, result.MountedBy[0].ReadOnly)
	assert.Equal(t, "rw-inst", result.MountedBy[1].Name)
	assert.False(t, result.MountedBy[1].ReadOnly)
}
