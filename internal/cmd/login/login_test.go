// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package login

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithOrganization(t *testing.T) {
	for _, tt := range []struct {
		name         string
		url          string
		organization string
		want         string
	}{
		{
			name: "no organization",
			url:  "https://console.unikraft.cloud/auth/verify?code=abc",
			want: "https://console.unikraft.cloud/auth/verify?code=abc",
		},
		{
			name:         "organization",
			url:          "https://console.unikraft.cloud/auth/verify?code=abc",
			organization: "acme",
			want:         "https://console.unikraft.cloud/auth/verify?code=abc&org=acme",
		},
		{
			name:         "escaped",
			url:          "https://console.unikraft.cloud/auth/verify?code=abc",
			organization: "a&b",
			want:         "https://console.unikraft.cloud/auth/verify?code=abc&org=a%26b",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, withOrganization(tt.url, tt.organization))
		})
	}
}
