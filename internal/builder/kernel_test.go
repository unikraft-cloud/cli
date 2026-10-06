// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

const fakeKernel = "not-really-a-kernel"

func writeKernel(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kernel")
	require.NoError(t, os.WriteFile(path, []byte(fakeKernel), 0o644))
	return path
}

func TestLoadKernel(t *testing.T) {
	opts := BuildOpts{
		Kernel:   writeKernel(t),
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs, err := LoadKernel(t.Context(), opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, img := range imgs {
			_ = img.Close()
		}
	})

	require.Len(t, imgs, 1)
	require.Equal(t, "kraftcloud/x86_64", platforms.Format(imgs[0].Image.Platform))
	require.NotNil(t, imgs[0].Kernel)

	r, _, err := imgs[0].Kernel.Open(context.Background())
	require.NoError(t, err)
	defer r.Close()
	contents, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, fakeKernel, string(contents))
}

func TestLoadKernelMultiplePlatforms(t *testing.T) {
	opts := BuildOpts{
		Kernel: writeKernel(t),
		Platform: []ocispec.Platform{
			{OS: "kraftcloud", Architecture: "x86_64"},
			{OS: "kraftcloud", Architecture: "arm64"},
		},
	}

	_, err := LoadKernel(t.Context(), opts)
	require.ErrorContains(t, err, "exactly one target, got 2")
}

func TestLoadKernelNoPlatform(t *testing.T) {
	opts := BuildOpts{Kernel: writeKernel(t)}

	_, err := LoadKernel(t.Context(), opts)
	require.ErrorContains(t, err, "exactly one target, got 0")
}

func TestLoadKernelMissing(t *testing.T) {
	opts := BuildOpts{
		Kernel:   filepath.Join(t.TempDir(), "absent"),
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	_, err := LoadKernel(t.Context(), opts)
	require.ErrorContains(t, err, "opening kernel")
}

func TestBuildKernelOnly(t *testing.T) {
	opts := BuildOpts{
		Kernel:   writeKernel(t),
		Cmd:      []string{"/server"},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs, err := Build(t.Context(), opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, img := range imgs {
			_ = img.Close()
		}
	})

	require.Len(t, imgs, 1)
	require.Equal(t, "kraftcloud/x86_64", platforms.Format(imgs[0].Image.Platform))
	require.NotNil(t, imgs[0].Kernel)
	require.Nil(t, imgs[0].Initrd)
	require.Equal(t, []string{"/server"}, imgs[0].Image.Config.Cmd)
}
