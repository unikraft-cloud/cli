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

	"unikraft.com/cli/pkg/model"
)

func TestImagesNotFound(t *testing.T) {
	require.NoError(t, imagesNotFound([]string{"a", "b"}, []bool{false, false}))

	err := errors.Join(errors.New("boom"), imagesNotFound([]string{"a", "b", "c"}, []bool{true, false, true}))
	notFound, ok := errors.AsType[model.ErrNotFound](err)
	require.True(t, ok)
	keys := make([]string, len(notFound.Keys))
	for i, key := range notFound.Keys {
		keys[i] = key.String()
	}
	assert.Equal(t, []string{"a", "c"}, keys)
}
