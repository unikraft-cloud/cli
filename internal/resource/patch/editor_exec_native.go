// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package patch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// CommandEditorFunc creates an EditorFunc that runs a shell command with YAML on stdin
// and reads the edited YAML from stdout.
func CommandEditorFunc(command string) EditorFunc {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		if runtime.GOOS == "windows" {
			return interpretCommand(ctx, command, input)
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Stdin = bytes.NewReader(input)
		cmd.Stderr = os.Stderr

		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("command exited with error: %w", err)
		}
		return output, nil
	}
}

// interpretCommand runs a shell command in the built-in interpreter, for
// systems that have no sh.
func interpretCommand(ctx context.Context, command string, input []byte) ([]byte, error) {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return nil, fmt.Errorf("parsing command: %w", err)
	}

	var output bytes.Buffer
	runner, err := interp.New(interp.StdIO(bytes.NewReader(input), &output, os.Stderr))
	if err != nil {
		return nil, err
	}
	if err := runner.Run(ctx, file); err != nil {
		return nil, fmt.Errorf("command exited with error: %w", err)
	}
	return output.Bytes(), nil
}

// VisualCommandEditorFunc creates an EditorFunc that uses a temp file and system editor.
func VisualCommandEditorFunc() (EditorFunc, error) {
	editor, err := getEditor()
	if err != nil {
		return nil, err
	}
	editorArgs, err := splitEditor(editor)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, input []byte) ([]byte, error) {
		tmpfile, err := os.CreateTemp("", "unikraft-edit-*.yaml")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		defer os.Remove(tmpfile.Name())

		if _, err := tmpfile.Write(input); err != nil {
			tmpfile.Close()
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		if err := tmpfile.Close(); err != nil {
			return nil, fmt.Errorf("failed to close temp file: %w", err)
		}

		cmd := exec.CommandContext(ctx, editorArgs[0], append(editorArgs[1:], tmpfile.Name())...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("editor exited with error: %w", err)
		}

		output, err := os.ReadFile(tmpfile.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read edited file: %w", err)
		}
		return output, nil
	}, nil
}

func getEditor() (string, error) {
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor, nil
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor, nil
	}
	if defaultEditor != "" {
		return defaultEditor, nil
	}
	return "", fmt.Errorf("no editor set: please set $VISUAL or $EDITOR")
}

// splitEditor splits an editor setting such as "code --wait" into a program
// and its arguments.  A setting that names a program as a whole is not split.
func splitEditor(editor string) ([]string, error) {
	if _, err := exec.LookPath(editor); err == nil {
		return []string{editor}, nil
	}
	args, err := editorFields(editor)
	if err != nil {
		return nil, fmt.Errorf("parsing editor %q: %w", editor, err)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("no editor set: please set $VISUAL or $EDITOR")
	}
	return args, nil
}
