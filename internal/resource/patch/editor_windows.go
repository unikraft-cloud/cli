// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package patch

import "golang.org/x/sys/windows"

// defaultEditor is the editor that every Windows installation has.
const defaultEditor = "notepad"

// editorFields splits an editor setting with the Windows command-line rules.
func editorFields(editor string) ([]string, error) {
	return windows.DecomposeCommandLine(editor)
}
