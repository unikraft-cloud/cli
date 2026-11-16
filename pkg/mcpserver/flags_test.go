// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"unikraft.com/cli/pkg/mcpserver"
)

func TestFlagsCheck(t *testing.T) {
	tests := []struct {
		flags mcpserver.Flags
		err   string
	}{
		{flags: mcpserver.Flags{}},
		{flags: mcpserver.Flags{Listen: "127.0.0.1:8080"}},
		{flags: mcpserver.Flags{Listen: "localhost:8080"}},
		{flags: mcpserver.Flags{Listen: "[::1]:8080"}},
		{flags: mcpserver.Flags{Listen: "0.0.0.0:8080", AllowRemote: true}},
		{flags: mcpserver.Flags{Listen: "0.0.0.0:8080"}, err: "pass --allow-remote"},
		{flags: mcpserver.Flags{Listen: "example.com:8080"}, err: "pass --allow-remote"},
		{flags: mcpserver.Flags{Listen: "localhost"}, err: "invalid listen address"},
		{flags: mcpserver.Flags{Listen: "localhost:http"}, err: "port must be a number"},
	}
	for _, tt := range tests {
		t.Run(tt.flags.Listen, func(t *testing.T) {
			err := tt.flags.Check()
			if tt.err == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.err)
			}
		})
	}
}
