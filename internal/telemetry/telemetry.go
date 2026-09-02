// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package telemetry provides basic analytics and crash reporting for the
// Unikraft CLI using PostHog.
package telemetry

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/posthog/posthog-go"

	"unikraft.com/x/fingerprint"
	"unikraft.com/x/version"

	"unikraft.com/cli/internal/config"
)

var (
	// PostHogAPIKey is the default PostHog project API key for CLI telemetry.
	// This can be overridden via the UNIKRAFT_POSTHOG_API_KEY environment variable.
	PostHogAPIKey = ""

	// PostHogHost is the PostHog API host.
	// This can be overridden via the UNIKRAFT_POSTHOG_HOST environment variable.
	PostHogHost = ""
)

var (
	distinctID string
	sessionID  string
	groups     posthog.Groups
	enabled    bool
	mu         sync.Mutex

	// commandStart tracks when the current command started for duration calculation.
	commandStart time.Time

	// commandPath stores the current command path for crash reporting.
	commandPath string
)

// EventPayload represents the data passed to the detached subprocess.
// Note: APIKey and Endpoint are intentionally excluded to avoid exposing
// them in process listings (ps/top). SendEvent reads them from package-level vars.
type EventPayload struct {
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	SessionID  string         `json:"session_id"`
	Properties map[string]any `json:"properties"`
	Groups     posthog.Groups `json:"groups,omitempty"`
	Timestamp  time.Time      `json:"timestamp"`
}

// Init initializes the PostHog analytics client for the given profile.
// If profile is nil, Init uses the anonymous machine fingerprint.
func Init(profile *config.Profile) error {
	mu.Lock()
	defer mu.Unlock()

	enabled = true

	apiKey := os.Getenv("UNIKRAFT_POSTHOG_API_KEY")
	if apiKey == "" {
		apiKey = PostHogAPIKey
	}
	if apiKey == "" {
		enabled = false
		return fmt.Errorf("no API key set for PostHog; use UNIKRAFT_POSTHOG_API_KEY environment variable")
	}

	// Use the user UUID when known, otherwise the machine fingerprint.
	distinctID = generateDistinctID()
	groups = nil
	if profile != nil {
		if profile.UserUUID != "" {
			distinctID = profile.UserUUID
		}
		if profile.OrganizationUUID != "" {
			groups = posthog.Groups{"organization": profile.OrganizationUUID}
		}
	}

	// Generate unique session ID for this CLI invocation.
	sessionID = generateSessionID()

	return nil
}

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

// generateDistinctID creates a unique session ID for this CLI invocation, which
// can be used to group events together.
func generateSessionID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// Fallback to hostname-based ID
		hostname, _ := os.Hostname()
		hash := sha256.Sum256([]byte(hostname + fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(hash[:16])
	}

	hash := sha256.Sum256(buf)
	return hex.EncodeToString(hash[:16])
}

// SendEvent processes an event payload in the detached subprocess.
// This is called by the hidden send-analytics subcommand.
func SendEvent(payloadJSON string) error {
	var payload EventPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return err
	}

	apiKey := os.Getenv("UNIKRAFT_POSTHOG_API_KEY")
	if apiKey == "" {
		apiKey = PostHogAPIKey
	}
	if apiKey == "" {
		return nil // No API key, so just skip sending analytics
	}

	host := os.Getenv("UNIKRAFT_POSTHOG_HOST")
	if host == "" {
		host = PostHogHost
	}

	// Create PostHog client - no need for fast timeouts since we're detached
	// Read API key and endpoint from package-level vars (not passed via argv for security)
	client, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint: host,
		// Use a short batch interval for CLI tools since they exit quickly
		BatchSize: 1,
		Interval:  100 * time.Millisecond,
		DefaultEventProperties: posthog.NewProperties().
			Set("cli_version", version.Version).
			Set("cli_commit", version.Commit).
			Set("cli_build_time", version.BuildTime).
			Set("os", runtime.GOOS).
			Set("arch", runtime.GOARCH).
			Set("ci", os.Getenv("CI") != "").
			Set("go_version", runtime.Version()),
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = client.Close()
	}()

	// Build properties
	props := posthog.NewProperties()
	for k, v := range payload.Properties {
		props.Set(k, v)
	}

	return client.Enqueue(posthog.Capture{
		DistinctId: payload.DistinctID,
		Event:      payload.Event,
		Properties: props,
		Groups:     payload.Groups,
		Timestamp:  payload.Timestamp,
	})
}
