// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/model/patch"
)

// TestCertificateEditNeedsChainAndPkey checks that edit returns an error
// when only chain or only pkey is set.
func TestCertificateEditNeedsChainAndPkey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec map[string][]string
	}{
		{
			name: "chain only",
			spec: map[string][]string{"chain": {"x"}},
		},
		{
			name: "pkey only",
			spec: map[string][]string{"pkey": {"y"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fields, err := cmd.Certificate{}.Fields(t.Context())
			require.NoError(t, err)

			patched, err := patch.PatchedFields(t.Context(), fields, patch.PatchSpec{Set: tt.spec})
			require.NoError(t, err)

			err = cmd.Certificate{}.Edit(t.Context(), "fra/demo-cert", patched)
			require.EqualError(t, err, "chain and pkey must be set together")
		})
	}
}
