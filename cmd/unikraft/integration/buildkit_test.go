// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildkit(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		r := runner(t, true, []string{stable})

		// The account's remote builds share the builder, so it is not deleted.
		r.Run(t, []string{"unikraft", "buildkit", "create", "--metro", r.MetroName})

		// Without --metro, the profile's only metro.
		out := r.Run(t, []string{"unikraft", "buildkit", "create"})
		assert.Regexp(t, `builder exists`, out)
	})

	t.Run("du-prune", func(t *testing.T) {
		r := runner(t, true, []string{stable})
		r.Run(t, []string{"unikraft", "buildkit", "create", "--metro", r.MetroName})

		out := r.Run(t, []string{"unikraft", "buildkit", "du", "--metro", r.MetroName})
		assert.Regexp(t, `(?m)^Reclaimable: +\S+\nTotal: +\S+$`, out)

		out = r.Run(t, []string{"unikraft", "buildkit", "prune", "--metro", r.MetroName})
		assert.Regexp(t, `pruned the builder's cache freed=\S+`, out)
	})
}
