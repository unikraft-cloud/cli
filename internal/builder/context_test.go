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
			want: map[string]string{
				"foo": "./foo",
			},
		},
		{
			name: "multiple contexts",
			values: []string{
				"foo=./foo",
				"bar=../bar",
			},
			want: map[string]string{
				"foo": "./foo",
				"bar": "../bar",
			},
		},
		{
			name:    "missing value",
			values:  []string{"foo"},
			wantErr: true,
		},
		{
			name:    "empty name",
			values:  []string{"=./foo"},
			wantErr: true,
		},
		{
			name:   "empty",
			values: []string{""},
			want:   map[string]string{},
		},
		{
			name:   "git context",
			values: []string{"scripts=https://github.com/user/repo.git"},
			want: map[string]string{
				"scripts": "https://github.com/user/repo.git",
			},
		},
		{
			name:   "image override context",
			values: []string{"alpine:3.23=docker-image://alpine:edge"},
			want: map[string]string{
				"alpine:3.23": "docker-image://alpine:edge",
			},
		},
		{
			name:   "target context",
			values: []string{"base=target:base"},
			want: map[string]string{
				"base": "target:base",
			},
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

func TestIsLocalBuildContext(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "relative path",
			value: "./foo",
			want:  true,
		},
		{
			name:  "parent relative path",
			value: "../foo",
			want:  true,
		},
		{
			name:  "absolute path",
			value: "/tmp/foo",
			want:  true,
		},
		{
			name:  "existing directory",
			value: t.TempDir(),
			want:  true,
		},
		{
			name:  "git url",
			value: "https://github.com/user/repo.git",
			want:  false,
		},
		{
			name:  "docker image reference",
			value: "docker-image://alpine:edge",
			want:  false,
		},
		{
			name:  "bake target reference",
			value: "target:base",
			want:  false,
		},
		{
			name:  "tarball url",
			value: "https://example.com/context.tar.gz",
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isLocalBuildContext(tt.value))
		})
	}
}
