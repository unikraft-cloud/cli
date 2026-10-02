// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js && !windows

package patch

import "mvdan.cc/sh/v3/shell"

// defaultEditor is empty, as there is no editor that every system has.
const defaultEditor = ""

// editorFields splits an editor setting with the shell rules.
func editorFields(editor string) ([]string, error) {
	return shell.Fields(editor, nil)
}
