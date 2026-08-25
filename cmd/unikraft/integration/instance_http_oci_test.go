// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"

	integ "unikraft.com/cli/internal/integration"
)

// testDigest is an arbitrary but well-formed digest, for the cases that need a
// digest nothing is ever fetched for.
const testDigest = "sha256:43d3d758e6fba7d4734ac142cfdbf8aa786fcbbfd828017eecaadc5140a4b190"

func TestInstancesHTTPOCI(t *testing.T) {
	// An http+oci image must reach the API with its scheme intact, since the
	// node is what fetches it. --dry-run stops before any request is made,
	// so this needs no metro.
	t.Run("dry-run", func(t *testing.T) {
		r := runner(t, false, []string{staging, stable, prod})

		for _, image := range []string{
			"http+oci://cdn.example.com/me/app/latest",
			"https+oci://cdn.example.com/me/app/@" + testDigest,
		} {
			// The --image shortcut and the generic field are separate paths into
			// the same value, so both have to carry the URI.
			for _, args := range [][]string{
				{"--set", "metro=fra", "--set", "name=test-" + uniq(), "--set", "image=" + image},
				{"--metro", "fra", "--name", "test-" + uniq(), "--image", image},
			} {
				out := r.Run(t, append([]string{
					"unikraft", "instance", "create", "--dry-run",
				}, args...))
				assert.Contains(t, out, image,
					"the image URI must reach the API verbatim (%v)", args)
			}
		}
	})

	// An unusable scheme has to say so.
	t.Run("reports an unusable scheme", func(t *testing.T) {
		r := runner(t, false, []string{staging, stable, prod})

		out := r.Run(t, []string{
			"unikraft", "instance", "create", "--dry-run",
			"--metro", "fra",
			"--name", "test-" + uniq(),
			"--image", "oci-archive:///tmp/image.tar",
		}, integ.ExpectFail())

		assert.Regexp(t, `addresses a local image`, out)
		assert.NotRegexp(t, `invalid reference format`, out,
			"a scheme this CLI cannot address is not a malformed reference")
	})
}
