// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package patch

import (
	"context"
	"errors"
)

// errNoEditor reports that the web console cannot launch an editor.
var errNoEditor = errors.New("editing with --visual or --cmd is not available in the web console; use --set, --load or --save -")

// CommandEditorFunc reports that running an editor needs a local shell.
func CommandEditorFunc(command string) EditorFunc {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		return nil, errNoEditor
	}
}

// VisualCommandEditorFunc reports that running an editor needs a local shell.
func VisualCommandEditorFunc() (EditorFunc, error) {
	return nil, errNoEditor
}
