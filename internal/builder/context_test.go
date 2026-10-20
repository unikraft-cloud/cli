// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseContextNames(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		want    map[string]string
		wantErr bool
	}{
		{
			name:   "single context",
			values: []string{"foo=./foo"},
			want:   map[string]string{"foo": "./foo"},
		},
		{
			name:   "multiple contexts",
			values: []string{"foo=./foo", "bar=../bar"},
			want:   map[string]string{"foo": "./foo", "bar": "../bar"},
		},
		{
			name:    "missing separator",
			values:  []string{"foo"},
			wantErr: true,
		},
		{
			name:    "empty name",
			values:  []string{"=./foo"},
			wantErr: true,
		},
		{
			name:    "empty value",
			values:  []string{"foo="},
			wantErr: true,
		},
		{
			name:    "whitespace-only name",
			values:  []string{"   =./foo"},
			wantErr: true,
		},
		{
			name:    "whitespace-only value",
			values:  []string{"foo=   "},
			wantErr: true,
		},
		{
			name:    "duplicate name",
			values:  []string{"foo=./foo", "foo=./other"},
			wantErr: true,
		},
		{
			name:   "git URL",
			values: []string{"scripts=https://github.com/user/repo.git"},
			want:   map[string]string{"scripts": "https://github.com/user/repo.git"},
		},
		{
			name:   "image context",
			values: []string{"alpine:3.23=docker-image://alpine:edge"},
			want:   map[string]string{"alpine:3.23": "docker-image://alpine:edge"},
		},
		{
			name:   "target context",
			values: []string{"base=target:base"},
			want:   map[string]string{"base": "target:base"},
		},
		{
			name:    "empty argument",
			values:  []string{""},
			wantErr: true,
		},
		{
			name:   "no contexts",
			values: nil,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseContextNames(tt.values)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
