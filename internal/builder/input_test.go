// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"unikraft.com/x/kraftfile"
)

const testKraftfile = `spec: v0.7
name: test
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["/server"]
targets:
  - platform: fc
    architecture: x86_64
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func TestLoadBuildOptsKraftfileDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Kraftfile", testKraftfile)
	writeFile(t, dir, "Dockerfile", "FROM scratch\n")

	opts, err := LoadBuildOpts(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, []string{"/server"}, opts.Cmd)
	require.Equal(t, filepath.Join(dir, "Dockerfile"), opts.Rootfs.Path)
	require.Equal(t, kraftfile.SourceTypeDockerfile, opts.Rootfs.Type)
	require.Equal(t, kraftfile.FsTypeErofs, opts.Rootfs.Format)
	require.Len(t, opts.Platform, 1)
	require.Equal(t, "x86_64", opts.Platform[0].Architecture)
}

func TestLoadBuildOptsKraftfilePath(t *testing.T) {
	dir := t.TempDir()
	kfPath := writeFile(t, dir, "kraft.yaml", testKraftfile)
	writeFile(t, dir, "Dockerfile", "FROM scratch\n")

	opts, err := LoadBuildOpts(t.Context(), kfPath)
	require.NoError(t, err)
	require.Equal(t, []string{"/server"}, opts.Cmd)
	require.Equal(t, filepath.Join(dir, "Dockerfile"), opts.Rootfs.Path,
		"rootfs path must resolve against the Kraftfile directory")
	require.Len(t, opts.Platform, 1)
}

func TestLoadBuildOptsKraftfilePrecedence(t *testing.T) {
	// A directory can hold more than one Kraftfile name. The name order of the
	// directory listing selects which one to use.
	want := []string{
		"Kraftfile",
		"Kraftfile.yaml",
		"Kraftfile.yml",
		"kraft.yaml",
		"kraft.yml",
	}

	dir := t.TempDir()
	for _, name := range want {
		writeFile(t, dir, name, "spec: v0.7\nruntime: marker/"+name+":latest\n")
	}

	// Remove the winner each time to show the full order.
	for _, name := range want {
		opts, err := LoadBuildOpts(t.Context(), dir)
		require.NoError(t, err)
		require.Equal(t, "marker/"+name+":latest", opts.Runtime)
		require.NoError(t, os.Remove(filepath.Join(dir, name)))
	}
}

func TestKraftfileNamesMatchUpstream(t *testing.T) {
	// The local list sets the precedence, but it must stay complete.
	require.ElementsMatch(t, kraftfile.DefaultFileNames, kraftfileNames)
}

func TestLoadBuildOptsDockerfileDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM scratch\n")

	opts, err := LoadBuildOpts(t.Context(), dir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "Dockerfile"), opts.Rootfs.Path)
	require.Equal(t, kraftfile.SourceTypeDockerfile, opts.Rootfs.Type)
	require.Empty(t, opts.Rootfs.Dockerfile)
	require.Empty(t, opts.Rootfs.Format, "format is chosen at build time")
	require.Empty(t, opts.Runtime)
	require.Empty(t, opts.Platform, "platforms come from --arch")
	require.Nil(t, opts.Cmd)
	require.Empty(t, opts.Roms)
}

func TestLoadBuildOptsDockerfilePath(t *testing.T) {
	dir := t.TempDir()
	dockerfile := writeFile(t, dir, "Dockerfile", "FROM scratch\n")

	opts, err := LoadBuildOpts(t.Context(), dockerfile)
	require.NoError(t, err)
	require.Equal(t, dockerfile, opts.Rootfs.Path)
	require.Equal(t, kraftfile.SourceTypeDockerfile, opts.Rootfs.Type)
}

func TestLoadBuildOptsCustomDockerfilePath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Kraftfile", testKraftfile)
	dockerfile := writeFile(t, dir, "app.Dockerfile", "FROM scratch\n")

	opts, err := LoadBuildOpts(t.Context(), dockerfile)
	require.NoError(t, err)
	require.Equal(t, dockerfile, opts.Rootfs.Path,
		"an explicit Dockerfile path must win over a Kraftfile in the same directory")
	require.Equal(t, kraftfile.SourceTypeDockerfile, opts.Rootfs.Type)
	require.Nil(t, opts.Cmd)
}

func TestLoadBuildOptsDockerfilePathSuffix(t *testing.T) {
	dir := t.TempDir()
	dockerfile := writeFile(t, dir, "Dockerfile.dev", "FROM scratch\n")

	opts, err := LoadBuildOpts(t.Context(), dockerfile)
	require.NoError(t, err)
	require.Equal(t, kraftfile.SourceTypeDockerfile, opts.Rootfs.Type)
}

func TestLoadBuildOptsMalformedKraftfileDoesNotFallBack(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Kraftfile", "spec: [broken\n")
	writeFile(t, dir, "Dockerfile", "FROM scratch\n")

	_, err := LoadBuildOpts(t.Context(), dir)
	require.Error(t, err, "a broken Kraftfile must not be masked by the Dockerfile")
}

func TestLoadBuildOptsCustomDockerfileNotDetectedInDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "app.Dockerfile", "FROM scratch\n")

	_, err := LoadBuildOpts(t.Context(), dir)
	require.ErrorContains(t, err, "no Kraftfile or Dockerfile found")
}

func TestLoadBuildOptsEmptyDirectory(t *testing.T) {
	_, err := LoadBuildOpts(t.Context(), t.TempDir())
	require.ErrorContains(t, err, "no Kraftfile or Dockerfile found")
}

func TestLoadBuildOptsNonexistent(t *testing.T) {
	_, err := LoadBuildOpts(t.Context(), filepath.Join(t.TempDir(), "missing"))
	require.ErrorContains(t, err, "does not exist")
}

func TestLoadBuildOptsNonKraftfileFile(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "notes.txt", "hello: [\n")

	_, err := LoadBuildOpts(t.Context(), p)
	require.Error(t, err, "a file that is not a Dockerfile is parsed as a Kraftfile")
}

func TestDockerfileToBuildOpts(t *testing.T) {
	opts := DockerfileToBuildOpts("/ctx/Dockerfile")
	require.Equal(t, "/ctx/Dockerfile", opts.Rootfs.Path)
	require.Equal(t, kraftfile.SourceTypeDockerfile, opts.Rootfs.Type)
	require.Empty(t, opts.Rootfs.Dockerfile)
}

func TestIsDockerfileName(t *testing.T) {
	for _, name := range []string{"Dockerfile", "app.Dockerfile", "Dockerfile.dev", "a.Dockerfile.b"} {
		require.True(t, isDockerfileName(name), name)
	}
	for _, name := range []string{"dockerfile", "Kraftfile", "MyDockerfile", "Dockerfiles", "Containerfile"} {
		require.False(t, isDockerfileName(name), name)
	}
}
