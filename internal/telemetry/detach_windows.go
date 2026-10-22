// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package telemetry

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func detach(cmd *exec.Cmd) {
	// A detached process has no console, so closing the console does not
	// stop it, and a new process group does not get Ctrl+C.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}

	// Don't hold the working directory
	cmd.Dir = os.TempDir()
}
