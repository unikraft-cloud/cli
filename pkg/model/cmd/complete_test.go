// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"testing"

	"github.com/posener/complete"
	"github.com/stretchr/testify/assert"

	modelt "unikraft.com/cli/internal/model/testing"
)

func TestPredictModelKey(t *testing.T) {
	ctx := modelt.WithTestEnv(t.Context(), &modelt.TestEnv{Store: modelt.BaseTestStore()})
	got := PredictModelKey[modelt.TestModel](ctx).Predict(complete.Args{})
	assert.ElementsMatch(t, []string{"test1", "test2"}, got)
}
