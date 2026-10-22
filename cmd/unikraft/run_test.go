// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	integ "unikraft.com/cli/internal/integration"
)

func TestInstanceCreateFlags(t *testing.T) {
	unikraftPath := integ.BuildUnikraft(t)
	t.Parallel()
	env := integ.NewTestEnv(t, unikraftPath)

	commands := [][]string{
		{"unikraft", "run"},
		{"unikraft", "instance", "run"},
		{"unikraft", "instance", "create"},
	}
	base := []string{"--metro=fra", "--image=nginx:latest", "--dry-run"}

	for _, command := range commands {
		args := slices.Concat(command, base)

		out := env.Run(t, slices.Concat(args, []string{"--rm"}))
		assert.Regexp(t, `features\s*:= \["delete-on-stop"\]`, out)

		out = env.Run(t, slices.Concat(args, []string{"--service=foo", "-p", "443:8080/http+tls"}), integ.ExpectFail())
		assert.Contains(t, out, "--publish cannot be used with --service")

		out = env.Run(t, slices.Concat(args, []string{"--service=foo", "--domain=example.com"}), integ.ExpectFail())
		assert.Contains(t, out, "--domain cannot be used with --service")
	}
}
