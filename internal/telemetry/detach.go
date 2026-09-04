// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build unix

package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"syscall"

	"github.com/posthog/posthog-go"
)

// spawnDetachedAnalytics spawns a detached subprocess to send analytics.
// On Unix, this uses process group detachment so the subprocess continues
// after the parent exits.
func spawnDetachedAnalytics(event posthog.Capture) {
	executable, err := os.Executable()
	if err != nil {
		return
	}

	payload, err := json.Marshal(EventPayload{
		Event:      event.Event,
		DistinctID: event.DistinctId,
		Properties: event.Properties,
		Groups:     event.Groups,
		Timestamp:  event.Timestamp,
	})
	if err != nil {
		return
	}

	cmd := exec.CommandContext(context.Background(), executable, "_send_analytics", string(payload))

	// Detach from parent process group so subprocess survives parent exit
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Don't hold the working directory
	cmd.Dir = "/"

	// Inherit environment (may be needed for network config)
	cmd.Env = os.Environ()

	// Start the process (non-blocking)
	if err := cmd.Start(); err != nil {
		return
	}

	// Release the process so it can run independently
	_ = cmd.Process.Release()
}
