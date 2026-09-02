// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package telemetry

import (
	"encoding/json"
	"testing"

	"github.com/posthog/posthog-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
)

func TestInitIdentity(t *testing.T) {
	t.Setenv("UNIKRAFT_POSTHOG_API_KEY", "test-key")

	machineID := generateDistinctID()
	require.Regexp(t, `^[0-9a-f]{32}$`, machineID)

	tests := []struct {
		name       string
		profile    *config.Profile
		distinctID string
		org        string
	}{
		{
			name:       "no profile uses the machine id",
			profile:    nil,
			distinctID: machineID,
		},
		{
			name:       "profile without uuids uses the machine id",
			profile:    &config.Profile{Organization: "acme"},
			distinctID: machineID,
		},
		{
			name:       "org only keeps the machine id and sets the group",
			profile:    &config.Profile{OrganizationUUID: "org-1"},
			distinctID: machineID,
			org:        "org-1",
		},
		{
			name:       "user and org use the user uuid and set the group",
			profile:    &config.Profile{UserUUID: "user-1", OrganizationUUID: "org-1"},
			distinctID: "user-1",
			org:        "org-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, Init(tt.profile))
			assert.Equal(t, tt.distinctID, distinctID)
			if tt.org == "" {
				assert.Nil(t, groups)
				return
			}
			assert.Equal(t, tt.org, groups["organization"])
		})
	}
}

func TestEventPayloadCarriesGroups(t *testing.T) {
	data, err := json.Marshal(EventPayload{
		Event:      "command_started",
		DistinctID: "user-1",
		Properties: posthog.NewProperties().Set("command", "metro list"),
		Groups:     posthog.Groups{"organization": "org-1"},
	})
	require.NoError(t, err)

	var payload EventPayload
	require.NoError(t, json.Unmarshal(data, &payload))

	assert.Equal(t, "command_started", payload.Event)
	assert.Equal(t, "user-1", payload.DistinctID)
	assert.Equal(t, "org-1", payload.Groups["organization"])
	assert.Equal(t, "metro list", payload.Properties["command"])
	assert.NotContains(t, payload.Properties, "$groups", "groups travel in their own field")
}
