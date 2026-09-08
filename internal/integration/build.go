// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	buildOnce       sync.Once
	buildBinaryDir  string
	buildBinaryPath string
	buildBinaryErr  error
)

// BuildUnikraft builds the unikraft CLI binary once and returns its path.
// Subsequent calls return the cached path without rebuilding.
func BuildUnikraft(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		binaryDir, err := os.MkdirTemp("", "unikraft-cli-test-*")
		if err != nil {
			buildBinaryErr = fmt.Errorf("create temp dir: %w", err)
			return
		}
		buildBinaryDir = binaryDir
		binaryName := "unikraft"
		if runtime.GOOS == "windows" {
			binaryName += ".exe"
		}
		binaryPath := filepath.Join(binaryDir, binaryName)
		// Mirrors the identity vars set by the `cli` task in Taskfile.yml.
		ldflags := strings.Join([]string{
			`-X unikraft.com/x/version.Name=unikraft-cli`,
			`-X unikraft.com/x/version.Docs=https://unikraft.com/docs/cli`,
			`-X unikraft.com/x/version.Issues=https://github.com/unikraft-cloud/cli/issues`,
		}, " ")
		cmd := exec.Command("go", "build", "-buildvcs=false", "-ldflags", ldflags, "-o", binaryPath, "unikraft.com/cli/cmd/unikraft")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			buildBinaryErr = fmt.Errorf("go build failed\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
			return
		}
		buildBinaryPath = binaryPath
	})
	require.NoError(t, buildBinaryErr)
	return buildBinaryPath
}

// CleanupUnikraftBinary deletes the temporary directory of the built binary.
func CleanupUnikraftBinary() {
	if buildBinaryDir == "" {
		return
	}
	if err := os.RemoveAll(buildBinaryDir); err != nil {
		fmt.Fprintf(os.Stderr, "failed to cleanup unikraft binary: %v\n", err)
	}
	buildBinaryDir = ""
}
