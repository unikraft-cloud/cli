// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"os"
	"testing"
)

// SkipUnlessEval skips the test when the eval build tag is not set or no
// API key is available.
func SkipUnlessEval(t testing.TB) {
	t.Helper()
	if !evalEnabled {
		t.Skip("skipping eval (missing eval build tag)")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("skipping eval (ANTHROPIC_API_KEY is not set)")
	}
}
