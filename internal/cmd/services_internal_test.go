// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"
)

func TestServiceRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		svc  Service
		want string
	}{
		{
			name: "tcp omits protocol and ip",
			svc:  Service{Source: 443, Destination: 8080, Handlers: []platform.ConnectionHandler{"tls"}},
			want: `{"port":443,"destination_port":8080,"handlers":["tls"]}`,
		},
		{
			name: "udp sends protocol and ip",
			svc:  Service{Source: 53, Destination: 53, Protocol: "udp", IP: "203.0.113.10"},
			want: `{"port":53,"destination_port":53,"protocol":"udp","ip":"203.0.113.10"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tt.svc.request())
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}
