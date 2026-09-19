// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package selector

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfirm pins that the question starts out answered "no": enter alone
// declines, and only moving onto "yes" first accepts.
func TestConfirm(t *testing.T) {
	for _, tt := range []struct {
		name string
		keys string
		yes  bool
	}{
		{"enter-declines", "\r", false},
		{"up-then-enter-accepts", "\x1b[A\r", true},
		{"down-stays-on-no", "\x1b[B\r", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			yes, err := confirm(t.Context(), strings.NewReader(tt.keys), &bytes.Buffer{}, "attach it", tea.WithoutSignalHandler())

			require.NoError(t, err)
			assert.Equal(t, tt.yes, yes)
		})
	}
}

// TestConfirmGivenUp pins that leaving the question is neither answer, so the
// caller can tell it from a no.
func TestConfirmGivenUp(t *testing.T) {
	yes, err := confirm(t.Context(), strings.NewReader("\x03"), &bytes.Buffer{}, "attach it", tea.WithoutSignalHandler())

	require.ErrorIs(t, err, ErrNoOptionSelected)
	assert.False(t, yes)
}
