// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"testing"
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
			name:   "empty",
			values: []string{""},
			want:   map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseContextNames(tt.values)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseContextNames() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Fatalf("got %v, want %v", got, tt.want)
				}

				for name, wantPath := range tt.want {
					if got[name] != wantPath {
						t.Errorf("context %q = %q, want %q", name, got[name], wantPath)
					}
				}
			}
		})
	}
}
