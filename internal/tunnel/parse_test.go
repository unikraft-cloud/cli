// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tunnel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Target
		wantErr string
	}{
		{
			name: "2-segment form",
			raw:  "my-instance:8080",
			want: Target{host: "my-instance", source: 0, dest: 8080, network: "tcp"},
		},
		{
			name: "2-segment form with metro prefix",
			raw:  "fra/my-instance:8080",
			want: Target{host: "fra/my-instance", source: 0, dest: 8080, network: "tcp"},
		},
		{
			name: "2-segment form with explicit tcp suffix",
			raw:  "my-instance:8080/tcp",
			want: Target{host: "my-instance", source: 0, dest: 8080, network: "tcp"},
		},
		{
			// Numeric instance names are unambiguous in the 2-segment form
			// (it's always INSTANCE:DEST_PORT), so they must be accepted
			// regardless of magnitude.
			name: "2-segment form with numeric instance name below 65535",
			raw:  "12345:8080",
			want: Target{host: "12345", source: 0, dest: 8080, network: "tcp"},
		},
		{
			name: "2-segment form with numeric instance name above 65535",
			raw:  "123456:8080",
			want: Target{host: "123456", source: 0, dest: 8080, network: "tcp"},
		},
		{
			name: "2-segment form with metro-prefixed numeric instance name",
			raw:  "fra/12345:8080",
			want: Target{host: "fra/12345", source: 0, dest: 8080, network: "tcp"},
		},
		{
			name: "3-segment form",
			raw:  "9000:my-instance:8080",
			want: Target{host: "my-instance", source: 9000, dest: 8080, network: "tcp"},
		},
		{
			name: "3-segment form with metro prefix",
			raw:  "9000:fra/my-instance:8080",
			want: Target{host: "fra/my-instance", source: 9000, dest: 8080, network: "tcp"},
		},
		{
			name: "3-segment form with explicit tcp suffix",
			raw:  "9000:my-instance:8080/tcp",
			want: Target{host: "my-instance", source: 9000, dest: 8080, network: "tcp"},
		},
		{
			name: "3-segment form with auto local port",
			raw:  "0:my-instance:8080",
			want: Target{host: "my-instance", source: 0, dest: 8080, network: "tcp"},
		},
		{
			name:    "udp suffix rejected",
			raw:     "my-instance:8080/udp",
			wantErr: `unsupported connection type "udp": only tcp is supported`,
		},
		{
			// "/foo" doesn't look like a protocol name, so it's treated as
			// part of the destination-port segment rather than stripped.
			name:    "unrecognized suffix is not treated as a protocol",
			raw:     "my-instance:8080/foo",
			wantErr: `"8080/foo" is not a valid port number`,
		},
		{
			name:    "no colon at all",
			raw:     "my-instance-only",
			wantErr: "not a valid forwarding target",
		},
		{
			name:    "4+ segments",
			raw:     "1:2:3:4",
			wantErr: `"3:4" is not a valid port number`,
		},
		{
			name:    "invalid dest port in 2-segment form",
			raw:     "my-instance:not-a-port",
			wantErr: `"not-a-port" is not a valid port number`,
		},
		{
			name:    "invalid local port in 3-segment form",
			raw:     "not-a-port:my-instance:8080",
			wantErr: `"not-a-port" is not a valid port number`,
		},
		{
			name:    "invalid dest port in 3-segment form",
			raw:     "9000:my-instance:not-a-port",
			wantErr: `"not-a-port" is not a valid port number`,
		},
		{
			name:    "empty string",
			raw:     "",
			wantErr: "not a valid forwarding target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTarget(tt.raw)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseProxyPorts(t *testing.T) {
	tests := []struct {
		name    string
		ports   []string
		want    []uint16
		wantErr string
	}{
		{name: "single valid port", ports: []string{"4444"}, want: []uint16{4444}},
		{name: "multiple valid ports", ports: []string{"4444", "4445"}, want: []uint16{4444, 4445}},
		{name: "not a number", ports: []string{"abc"}, wantErr: `"abc" is not a valid port number`},
		{name: "negative", ports: []string{"-1"}, wantErr: `"-1" is not a valid port number`},
		{name: "overflows uint16", ports: []string{"65536"}, wantErr: `"65536" is not a valid port number`},
		{name: "empty input", ports: nil, want: []uint16{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProxyPorts(tt.ports)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseTargets(t *testing.T) {
	t.Run("no targets", func(t *testing.T) {
		_, err := ParseTargets(nil, []string{"4444"})
		require.Error(t, err)
		assert.ErrorContains(t, err, "at least one target must be specified")
	})

	t.Run("no proxy ports", func(t *testing.T) {
		_, err := ParseTargets([]string{"a:80"}, nil)
		require.Error(t, err)
		assert.ErrorContains(t, err, "at least one proxy port must be specified")
	})

	t.Run("invalid target propagates parse error", func(t *testing.T) {
		_, err := ParseTargets([]string{"not-a-valid-target"}, []string{"4444"})
		require.Error(t, err)
		assert.ErrorContains(t, err, `parsing target "not-a-valid-target"`)
	})

	t.Run("invalid proxy port propagates parse error", func(t *testing.T) {
		_, err := ParseTargets([]string{"a:80"}, []string{"not-a-port"})
		require.Error(t, err)
		assert.ErrorContains(t, err, `"not-a-port" is not a valid port number`)
	})

	t.Run("port count mismatch", func(t *testing.T) {
		_, err := ParseTargets([]string{"a:80", "b:80"}, []string{"4444", "5555", "6666"})
		require.Error(t, err)
		assert.ErrorContains(t, err, "number of proxy ports must be either 1 or equal to the number of targets")
	})

	t.Run("sequential assignment overflows uint16", func(t *testing.T) {
		_, err := ParseTargets([]string{"a:80", "b:80"}, []string{"65535"})
		require.Error(t, err)
		assert.ErrorContains(t, err, "does not leave enough room")
	})

	t.Run("sequential assignment at the boundary succeeds", func(t *testing.T) {
		got, err := ParseTargets([]string{"a:80", "b:80"}, []string{"65534"})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.EqualValues(t, 65534, got[0].exposedProxyPort)
		assert.EqualValues(t, 65535, got[1].exposedProxyPort)
	})

	t.Run("sequential assignment", func(t *testing.T) {
		got, err := ParseTargets([]string{"a:80", "b:80", "c:80"}, []string{"4444"})
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.EqualValues(t, 4444, got[0].exposedProxyPort)
		assert.EqualValues(t, 4445, got[1].exposedProxyPort)
		assert.EqualValues(t, 4446, got[2].exposedProxyPort)
	})

	t.Run("explicit per-target ports", func(t *testing.T) {
		got, err := ParseTargets([]string{"a:80", "b:80"}, []string{"5000", "6000"})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.EqualValues(t, 5000, got[0].exposedProxyPort)
		assert.EqualValues(t, 6000, got[1].exposedProxyPort)
	})

	t.Run("duplicate explicit per-target ports rejected", func(t *testing.T) {
		_, err := ParseTargets([]string{"a:80", "b:80"}, []string{"5000", "5000"})
		require.Error(t, err)
		assert.ErrorContains(t, err, "duplicate proxy port 5000")
	})
}

func TestFormatProxyArgs(t *testing.T) {
	targets := []resolvedTarget{
		{dest: 80, exposedProxyPort: 4444, network: "tcp", ip: "10.0.0.1"},
		{dest: 8080, exposedProxyPort: 4445, network: "tcp", ip: "10.0.0.2"},
	}
	args := formatProxyArgs(targets, "authcookie", 4443)
	require.Len(t, args, 4)
	assert.Equal(t, "4443:5", args[0])
	assert.Equal(t, "5:authcookie", args[1])
	assert.Equal(t, "600", args[2])
	assert.Equal(t, "[TCP2TCP:10.0.0.1:80:4444:27|TCP2TCP:10.0.0.2:8080:4445:27]", args[3])
}
