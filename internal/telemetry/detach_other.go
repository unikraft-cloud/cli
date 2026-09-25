// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !unix && !windows

package telemetry

import "github.com/posthog/posthog-go"

// spawnDetachedAnalytics is a no-op on platforms that cannot detach a
// subprocess.  Telemetry is best-effort, so it is skipped there.
func spawnDetachedAnalytics(posthog.Capture) {
	// No-op: detached subprocess spawning not implemented for this platform
}
