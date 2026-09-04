// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package telemetry

import (
	"fmt"
	"runtime"
	"time"

	"github.com/posthog/posthog-go"
)

// TrackCrash records panic/crash information for debugging.
func TrackCrash(panicValue any, stack []byte) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return
	}

	duration := time.Since(commandStart)

	props := posthog.NewProperties().
		Set("command", commandPath).
		Set("duration_ms", duration.Milliseconds()).
		Set("panic_type", fmt.Sprintf("%T", panicValue)).
		Set("panic_value", sanitizeErrorMessage(fmt.Sprintf("%v", panicValue))).
		Set("stack_trace", stack)

	spawnDetachedAnalytics(posthog.Capture{
		DistinctId: distinctID,
		Event:      "cli_crash",
		Properties: props,
		Groups:     groups,
	})
}

// RecoverAndReport should be deferred at the top of main() to catch panics,
// report them to PostHog, and then re-panic.
func RecoverAndReport() {
	if r := recover(); r != nil {
		// Capture stack trace
		stack := make([]byte, 4096)
		n := runtime.Stack(stack, false)
		stack = stack[:n]
		if n > 4096 {
			stack = append(stack, []byte("\n[Truncated stack trace]")...)
		}

		TrackCrash(r, stack)

		// Re-panic to preserve the original behavior
		panic(r)
	}
}

// TrackCommandStart records when a command starts executing.
func TrackCommandStart(cmdPath string) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return
	}

	commandPath = cmdPath
	commandStart = time.Now()

	spawnDetachedAnalytics(posthog.Capture{
		DistinctId: distinctID,
		Event:      "command_started",
		Properties: posthog.NewProperties().
			Set("command", cmdPath).
			Set("session_id", sessionID),
		Groups: groups,
	})
}

// TrackCommandSuccess records successful command completion with duration.
func TrackCommandSuccess(cmdPath string) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return
	}

	duration := time.Since(commandStart)

	spawnDetachedAnalytics(posthog.Capture{
		DistinctId: distinctID,
		Event:      "command_succeeded",
		Properties: posthog.NewProperties().
			Set("command", cmdPath).
			Set("duration_ms", duration.Milliseconds()).
			Set("session_id", sessionID),
		Groups: groups,
	})
}

// TrackCommandError records command failures with error information.
func TrackCommandError(cmdPath string, err error) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return
	}

	duration := time.Since(commandStart)

	props := posthog.NewProperties().
		Set("command", cmdPath).
		Set("duration_ms", duration.Milliseconds()).
		Set("session_id", sessionID)

	if err != nil {
		// Only capture error type, not the full message which may contain sensitive data
		props.Set("error_type", fmt.Sprintf("%T", err))
		// Capture a sanitized error message (first line only, truncated)
		errMsg := sanitizeErrorMessage(err.Error())
		props.Set("error_message", errMsg)
	}

	spawnDetachedAnalytics(posthog.Capture{
		DistinctId: distinctID,
		Event:      "command_failed",
		Properties: props,
		Groups:     groups,
	})
}
