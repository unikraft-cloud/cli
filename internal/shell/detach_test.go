// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetachPolicy(t *testing.T) {
	t.Run("a-statement-is-waited-for", func(t *testing.T) {
		transport := newHaltingTransport()
		s, _ := newHaltingSession(t, transport)

		sigint := make(chan os.Signal, 4)
		interruptOnce(transport, sigint)
		runShellLine(t, s, sigint, blockingCommand)

		assert.False(t, transport.lastDetached())
	})

	t.Run("a-probe-costs-nothing", func(t *testing.T) {
		transport := newHaltingTransport()
		s, _ := newHaltingSession(t, transport)

		_, err := s.script(t.Context(), statScript, "/tmp")
		require.NoError(t, err)
		assert.True(t, transport.lastDetached())
	})

	t.Run("a-single-command-line-is-waited-for-too", func(t *testing.T) {
		transport := newHaltingTransport()
		require.NoError(t, Run(t.Context(), Config{
			Instance:  "fake",
			Dir:       "/",
			Command:   "once",
			Transport: transport,
		}, Streams{In: strings.NewReader(""), Out: &captured{}, Err: &captured{}}))

		assert.False(t, transport.lastDetached())
	})
}
