// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import (
	"context"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// defaultEditor is the editor that every Windows installation has.
const defaultEditor = "notepad"

// editorFields splits an editor setting with the Windows command-line rules.
func editorFields(editor string) ([]string, error) {
	return windows.DecomposeCommandLine(editor)
}

// shellCommand runs a command line in cmd.exe.  cmd.exe parses its command line
// with its own rules, thus the line goes to it as is.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /S /C "` + command + `"`}
	return cmd
}
