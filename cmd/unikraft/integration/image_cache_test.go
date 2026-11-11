// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestImageCache(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		r := runner(t, true, []string{staging, stable})

		out := r.Run(t, []string{"unikraft", "image", "list", "--cache"})
		assert.NotContains(t, out, "level=error")
	})

	t.Run("pin-edit-unpin", func(t *testing.T) {
		r := runner(t, true, []string{staging})

		image := r.Profile.Organization + "/nginx-pin:" + uniq()
		r.Run(t, []string{"unikraft", "image", "copy", "nginx:latest", image})
		t.Cleanup(func() {
			r.Run(t, []string{"unikraft", "image", "unpin", image, "--metro", r.MetroName}, integ.WithoutCancel(), integ.AllowFail())
		})

		out, err := r.RunRaw(t, []string{"unikraft", "image", "pin", image, "--metro", r.MetroName, "--autokill", "1h"})
		if err != nil && strings.Contains(out, "No API endpoint") {
			t.Skip("image pinning is not available on " + r.MetroName)
		}
		require.NoError(t, err, out)

		out = r.Run(t, []string{"unikraft", "image", "get", "--cache", image})
		assert.Regexp(t, `pinned:\s+\["`+r.MetroName+`"\]`, out)
		assert.Regexp(t, r.MetroName+`:\s+1h0m0s`, out)

		r.Run(t, []string{"unikraft", "image", "edit", image, "--add", "autokill=" + r.MetroName + "=2h"})
		out = r.Run(t, []string{"unikraft", "image", "get", "--cache", image})
		assert.Regexp(t, r.MetroName+`:\s+2h0m0s`, out)

		r.Run(t, []string{"unikraft", "image", "edit", image, "--del", "autokill=" + r.MetroName})
		out = r.Run(t, []string{"unikraft", "image", "get", "--cache", image})
		assert.Regexp(t, `pinned:\s+\["`+r.MetroName+`"\]`, out)
		assert.NotContains(t, out, "autokill:")

		r.Run(t, []string{"unikraft", "image", "unpin", image, "--metro", r.MetroName})
		out = r.Run(t, []string{"unikraft", "image", "get", "--cache", image})
		assert.Regexp(t, `active:\s+\["`+r.MetroName+`"\]`, out)
		assert.NotContains(t, out, "pinned:")
	})
}
