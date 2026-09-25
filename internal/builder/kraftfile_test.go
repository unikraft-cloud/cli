// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"unikraft.com/x/kraftfile"
)

func TestKraftfileToBuildOpts(t *testing.T) {
	rootfsDir := t.TempDir()
	rootfsPath := "Dockerfile"

	runtime := kraftfile.Runtime("unikraft.io/unikraft.org/base")
	kf := &kraftfile.Kraftfile{
		Cmd:     kraftfile.Command{"/server", "--flag"},
		Env:     kraftfile.Map{{Key: "A", Value: "1"}},
		Labels:  map[string]string{"label": "value"},
		Runtime: &runtime,
		Rootfs: &kraftfile.FS{
			Format: kraftfile.FsTypeErofs,
			Source: &kraftfile.FSSource{
				Path: rootfsPath,
			},
		},
		Targets: []kraftfile.Target{
			{
				Arch: "x86_64",
				Plat: "fc",
				KConfig: kraftfile.Map{
					{Key: "CONFIG_UK_FULLVERSION", Value: "1.2.3"},
					{Key: "CONFIG_DEBUG", Value: "y"},
				},
			},
		},
	}

	opts, err := KraftfileToBuildOpts(rootfsDir, kf)
	require.NoError(t, err)
	require.Equal(t, []string{"/server", "--flag"}, opts.Cmd)
	require.Equal(t, kraftfile.Map{{Key: "A", Value: "1"}}, opts.Env)
	require.Equal(t, map[string]string{"label": "value"}, opts.Labels)
	require.Equal(t, "unikraft.io/unikraft.org/base", opts.Runtime)
	require.Equal(t, kraftfile.FsTypeErofs, opts.Rootfs.Format)
	require.Equal(t, filepath.Join(rootfsDir, "Dockerfile"), opts.Rootfs.Path)
	require.Equal(t, kraftfile.SourceTypeDockerfile, opts.Rootfs.Type)
	require.Len(t, opts.Platform, 1)
	require.Equal(t, "x86_64", opts.Platform[0].Architecture)
	require.Equal(t, "fc", opts.Platform[0].OS)
	require.Equal(t, "1.2.3", opts.Platform[0].OSVersion)
	require.Equal(t, []string{
		"CONFIG_UK_FULLVERSION=1.2.3",
		"CONFIG_DEBUG=y",
	}, opts.Platform[0].OSFeatures)
}

func TestKraftfileToBuildOptsRootfsSourceError(t *testing.T) {
	rootfsDir := t.TempDir()
	rootfsPath := "rootfs.tar"

	runtime := kraftfile.Runtime("unikraft.io/unikraft.org/base")
	kf := &kraftfile.Kraftfile{
		Runtime: &runtime,
		Rootfs: &kraftfile.FS{
			Format: kraftfile.FsTypeCpio,
			Source: &kraftfile.FSSource{
				Path: rootfsPath,
			},
		},
	}

	_, err := KraftfileToBuildOpts(rootfsDir, kf)
	require.Error(t, err)
}

func TestKraftfileToBuildOptsRootfsPathJoined(t *testing.T) {
	rootfsDir := t.TempDir()

	runtime := kraftfile.Runtime("unikraft.io/unikraft.org/base")
	kf := &kraftfile.Kraftfile{
		Runtime: &runtime,
		Rootfs: &kraftfile.FS{
			Format: kraftfile.FsTypeErofs,
			Source: &kraftfile.FSSource{
				Path: "Dockerfile",
			},
		},
	}

	opts, err := KraftfileToBuildOpts(rootfsDir, kf)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(rootfsDir, "Dockerfile"), opts.Rootfs.Path,
		"rootfs path must be joined with the kraftfile directory")
}

func TestKraftfileToBuildOptsDockerfileWithType(t *testing.T) {
	rootfsDir := t.TempDir()

	runtime := kraftfile.Runtime("unikraft.io/unikraft.org/base")
	kf := &kraftfile.Kraftfile{
		Runtime: &runtime,
		Rootfs: &kraftfile.FS{
			Format: kraftfile.FsTypeErofs,
			Source: &kraftfile.FSSource{
				Path:       "context",
				Dockerfile: "MyDockerfile",
				Type:       kraftfile.SourceTypeDockerfile,
			},
		},
	}

	opts, err := KraftfileToBuildOpts(rootfsDir, kf)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(rootfsDir, "context"), opts.Rootfs.Path)
	require.Equal(t, "MyDockerfile", opts.Rootfs.Dockerfile)
	require.Equal(t, kraftfile.SourceTypeDockerfile, opts.Rootfs.Type)
	require.Equal(t, kraftfile.FsTypeErofs, opts.Rootfs.Format)
}

func TestKraftfileToBuildOptsDockerfileWithoutType(t *testing.T) {
	rootfsDir := t.TempDir()

	runtime := kraftfile.Runtime("unikraft.io/unikraft.org/base")
	kf := &kraftfile.Kraftfile{
		Runtime: &runtime,
		Rootfs: &kraftfile.FS{
			Format: kraftfile.FsTypeErofs,
			Source: &kraftfile.FSSource{
				Path:       "context",
				Dockerfile: "MyDockerfile",
			},
		},
	}

	opts, err := KraftfileToBuildOpts(rootfsDir, kf)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(rootfsDir, "context"), opts.Rootfs.Path)
	require.Equal(t, "MyDockerfile", opts.Rootfs.Dockerfile)
	require.Equal(t, kraftfile.SourceTypeDockerfile, opts.Rootfs.Type,
		"type must be inferred as dockerfile when dockerfile field is set")
	require.Equal(t, kraftfile.FsTypeErofs, opts.Rootfs.Format)
}

func TestKraftfileToBuildOptsNoRootfs(t *testing.T) {
	rootfsDir := t.TempDir()

	runtime := kraftfile.Runtime("unikraft.io/unikraft.org/base")
	kf := &kraftfile.Kraftfile{
		Cmd:     kraftfile.Command{"/server"},
		Runtime: &runtime,
		Targets: []kraftfile.Target{
			{
				Arch: "x86_64",
				Plat: "fc",
			},
		},
	}

	opts, err := KraftfileToBuildOpts(rootfsDir, kf)
	require.NoError(t, err)
	require.Equal(t, "unikraft.io/unikraft.org/base", opts.Runtime)
	require.Empty(t, opts.Rootfs.Path)
	require.False(t, opts.Rootfs.Compress)
	require.Equal(t, []string{"/server"}, opts.Cmd)
	require.Len(t, opts.Platform, 1)
	require.Equal(t, "x86_64", opts.Platform[0].Architecture)
	require.Equal(t, "fc", opts.Platform[0].OS)
}
