// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
)

// generateDistinctID returns a fixed anonymous ID.  The browser exposes no
// machine characteristics to fingerprint.
func generateDistinctID() string {
	hash := sha256.Sum256([]byte("web-console-unikraft-cli"))
	return hex.EncodeToString(hash[:16])
}
