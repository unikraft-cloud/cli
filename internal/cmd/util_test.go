// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"
)

func partialDeleteResponse(t *testing.T, codes ...platform.APIHTTPError) error {
	t.Helper()

	volumes := make([]platform.DeleteVolumesResponseDeletedVolume, 0, len(codes))
	for _, code := range codes {
		msg := "failed"
		code := int32(code)
		volumes = append(volumes, platform.DeleteVolumesResponseDeletedVolume{
			Status:  platform.ResponseStatusError,
			Message: &msg,
			Error:   &code,
		})
	}
	err := platform.NewFromResponse(&platform.Response[platform.DeleteVolumesResponseData]{
		Status: platform.ResponseStatusPartialSuccess,
		Data: &platform.DeleteVolumesResponseData{
			Volumes: volumes,
		},
	})
	require.Error(t, err)
	return err
}

func TestIgnoreNotFound(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		assert.NoError(t, ignoreNotFound(nil))
	})

	t.Run("only_not_found", func(t *testing.T) {
		err := partialDeleteResponse(t, platform.APIHTTPErrorNotFound, platform.APIHTTPErrorNotFound)
		assert.NoError(t, ignoreNotFound(err))
	})

	t.Run("mixed_with_not_found", func(t *testing.T) {
		err := partialDeleteResponse(t, platform.APIHTTPErrorNotFound, platform.APIHTTPErrorQuota)
		assert.ErrorIs(t, ignoreNotFound(err), err)
	})

	t.Run("other", func(t *testing.T) {
		err := errors.New("boom")
		assert.ErrorIs(t, ignoreNotFound(err), err)
	})
}
