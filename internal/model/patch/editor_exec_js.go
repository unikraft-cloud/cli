// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package patch

import (
	"context"
	"errors"
)

// errNoEditor reports that the js build cannot launch an editor.
var errNoEditor = errors.New("editing with --visual or --cmd is only available in the native unikraft CLI; use --set, --load or --save -")

// CommandEditorFunc creates an EditorFunc that returns errNoEditor.
func CommandEditorFunc(command string) EditorFunc {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		return nil, errNoEditor
	}
}

// VisualCommandEditorFunc returns errNoEditor.
func VisualCommandEditorFunc() (EditorFunc, error) {
	return nil, errNoEditor
}
