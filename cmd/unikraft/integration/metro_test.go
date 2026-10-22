// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetros(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		out := r.Run(t, []string{"unikraft", "metro", "get", r.MetroName})
		assert.Contains(t, out, "name:")
		assert.Contains(t, out, r.MetroName)
		assert.Contains(t, out, "endpoint:")
	})

	t.Run("get-quotas", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		out := r.Run(t, []string{"unikraft", "metro", "get", r.MetroName, "-f", "+quotas"})
		assert.Contains(t, out, "quotas:")
		assert.Contains(t, out, "instances:")
		assert.Contains(t, out, "vcpus:")
	})
}
