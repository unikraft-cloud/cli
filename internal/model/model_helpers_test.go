// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package model_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/model"
	modelt "unikraft.com/cli/internal/model/testing"
)

func TestGetFromListable(t *testing.T) {
	ctx := modelt.WithTestEnv(t.Context(), &modelt.TestEnv{Store: modelt.BaseTestStore()})

	found, err := model.GetFromListable(ctx, modelt.TestModel{}, []string{"test2"})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "test2", found[0].Key().String())

	found, err = model.GetFromListable(ctx, modelt.TestModel{}, []string{"test1", "nope"})
	require.Len(t, found, 1)
	assert.Equal(t, "test1", found[0].Key().String())
	notFound, ok := errors.AsType[model.ErrNotFound](err)
	require.True(t, ok)
	assert.Equal(t, []model.Key{model.StringKey("nope")}, notFound.Keys)
}
