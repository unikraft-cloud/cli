// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js && !windows

package cmd

import (
	"testing"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/x/stdio"
)

func TestExecRunsOnAPtyFromATerminal(t *testing.T) {
	for _, exec := range []ExecSandboxInstanceCmd{{}, {TTY: true}, {NoTTY: true}} {
		ptmx, tty, err := pty.Open()
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = ptmx.Close()
			_ = tty.Close()
		})

		fake := &onePlugin{}
		exec.Cmd = []string{"sh"}
		err = exec.runOn(t.Context(), fakeSandboxTarget(t, fake), stdio.Stdio{Stdin: tty, Stdout: tty, Stderr: tty})

		require.NoError(t, err)
		fake.mu.Lock()
		assert.Equal(t, !exec.NoTTY, fake.pty, "--tty=%v --no-tty=%v", exec.TTY, exec.NoTTY)
		fake.mu.Unlock()
	}
}
