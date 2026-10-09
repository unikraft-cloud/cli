// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractBinary(t *testing.T) {
	binaryName := "unikraft"
	if runtime.GOOS == "windows" {
		binaryName = "unikraft.exe"
	}
	entries := map[string]string{
		"docs/man/unikraft.1.gz":     "man page",
		"unikraft-cli/" + binaryName: "new binary",
	}

	dir := t.TempDir()

	tarPath := filepath.Join(dir, "unikraft-linux-amd64.tar.gz")
	tarFile, err := os.Create(tarPath)
	require.NoError(t, err)
	gzw := gzip.NewWriter(tarFile)
	tw := tar.NewWriter(gzw)
	for name, data := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(data))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gzw.Close())
	require.NoError(t, tarFile.Close())

	zipPath := filepath.Join(dir, "unikraft-windows-amd64.zip")
	zipFile, err := os.Create(zipPath)
	require.NoError(t, err)
	zw := zip.NewWriter(zipFile)
	for name, data := range entries {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(data))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, zipFile.Close())

	for _, archive := range []string{tarPath, zipPath} {
		t.Run(filepath.Base(archive), func(t *testing.T) {
			dest := t.TempDir()
			got, err := extractBinary(archive, dest)
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(dest, binaryName), got)

			data, err := os.ReadFile(got)
			require.NoError(t, err)
			assert.Equal(t, "new binary", string(data), "only the binary is extracted")
		})
	}
}

func TestRemoveUpgradeBackup(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only an upgrade on Windows keeps the old binary")
	}

	execPath, err := os.Executable()
	require.NoError(t, err)
	execPath, err = filepath.EvalSymlinks(execPath)
	require.NoError(t, err)
	backupPath := upgradeBackupPath(execPath)
	require.NoError(t, os.WriteFile(backupPath, []byte("old binary"), 0o755))

	RemoveUpgradeBackup()

	assert.NoFileExists(t, backupPath)
}

func TestInstallBinaryBusyBackup(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows cannot replace a file in use")
	}

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "new.exe")
	destPath := filepath.Join(dir, "unikraft.exe")
	require.NoError(t, os.WriteFile(srcPath, []byte("new binary"), 0o755))
	require.NoError(t, os.WriteFile(destPath, []byte("current binary"), 0o755))
	require.NoError(t, os.WriteFile(upgradeBackupPath(destPath), []byte("old binary"), 0o755))

	// An open file stands in for a process that still runs the old binary.
	busy, err := os.Open(upgradeBackupPath(destPath))
	require.NoError(t, err)
	defer busy.Close()

	require.NoError(t, installBinary(srcPath, destPath))

	data, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, "new binary", string(data))
}
