// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"os"
	"testing"

	"unikraft.com/cli/internal/cmd"
	integ "unikraft.com/cli/internal/integration"
)

func TestMain(m *testing.M) {
	code := m.Run()
	integ.Cleanup()
	os.Exit(code)
}

// Servers
const (
	staging = "staging"
	stable  = "stable"
	prod    = "prod"
)

func uniq() string {
	return integ.Uniq()
}

func runner(t *testing.T, online bool, servers []string) *integ.TestEnv {
	t.Helper()
	integ.SkipUnlessIntegration(t)
	integ.SkipUnlessSupportedServer(t, servers)
	t.Parallel()
	return integ.NewEnv(t, online, cmd.PartitionedResources...)
}
