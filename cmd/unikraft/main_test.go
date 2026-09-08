// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"os"
	"testing"

	integ "unikraft.com/cli/internal/integration"
)

func TestMain(m *testing.M) {
	code := m.Run()
	integ.CleanupUnikraftBinary()
	os.Exit(code)
}
