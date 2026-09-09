// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"unikraft.com/x/fingerprint"
)

// generateDistinctID creates an anonymous distinct ID based on machine fingerprint.
// The ID is a SHA-256 hash to ensure privacy while maintaining consistency.
func generateDistinctID() string {
	fp, err := fingerprint.New()
	if err != nil {
		// Fallback to hostname-based ID
		hostname, _ := os.Hostname()
		hash := sha256.Sum256([]byte(hostname + "-unikraft-cli"))
		return hex.EncodeToString(hash[:16])
	}

	// Create a stable fingerprint string from machine characteristics
	fpStr := fmt.Sprintf("%s-%s-%s-%s-%t",
		fp.Hostname,
		fp.Os,
		fp.Goarch,
		fp.Goos,
		fp.Container,
	)
	hash := sha256.Sum256([]byte(fpStr))
	return hex.EncodeToString(hash[:16])
}
