// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/cli/internal/builder/buildfs"
	"unikraft.com/cli/internal/integration"
	"unikraft.com/x/kraftfile"
)

// writeUnikraftOCIArchive packages srcDir into a CPIO initrd, wraps it in a
// unikraft-style OCI image (with a dedicated initrd component), and saves the
// result as an OCI archive tarball. Extra image options, e.g. a config to
// inherit, are appended. It returns the path to the tarball.
func writeUnikraftOCIArchive(t *testing.T, srcDir string, extra ...imagespec.NewImageOpt) string {
	t.Helper()

	cpioPath := filepath.Join(t.TempDir(), "initrd.cpio")
	f, err := os.Create(cpioPath)
	require.NoError(t, err)
	defer f.Close()

	ctx := context.Background()
	require.NoError(t, buildfs.CreateCPIO(ctx, f, os.DirFS(srcDir)))
	require.NoError(t, f.Sync())

	cpioFile, err := os.Open(cpioPath)
	require.NoError(t, err)
	t.Cleanup(func() { cpioFile.Close() })

	img := imagespec.NewImage(append([]imagespec.NewImageOpt{
		imagespec.WithPlatform(ocispec.Platform{OS: "fc", Architecture: "x86_64"}),
		imagespec.WithInitrd(imagespec.NewOSFile(cpioFile)),
	}, extra...)...)

	archivePath := filepath.Join(t.TempDir(), "unikraft-image.tar")
	require.NoError(t, imagespec.SaveTarball(ctx, archivePath, img))
	return archivePath
}

// TestRootfsOCIUnikraftImage reads a unikraft-style OCI image that carries a
// dedicated initrd component and verifies that the initrd is passed through
// untouched, without being repackaged.
func TestRootfsOCIUnikraftImage(t *testing.T) {
	srcDir := writeTestDirectory(t)
	archivePath := writeUnikraftOCIArchive(t, srcDir)

	for _, format := range []kraftfile.FsType{"", kraftfile.FsTypeCpio} {
		t.Run(cmp.Or(string(format), "unset"), func(t *testing.T) {
			imgs := runBuildRootfs(t, BuildOpts{
				Rootfs: FSOpts{
					Path:   "oci-archive://" + archivePath,
					Type:   kraftfile.SourceTypeOCI,
					Format: format,
				},
				Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
			})
			require.Len(t, imgs, 1)

			files := readCpioInitrd(t, imgs[0])
			require.Contains(t, files, "./hello.txt")
			require.Equal(t, "hello\n", files["./hello.txt"])
			require.Contains(t, files, "./subdir/nested.txt")
			require.Equal(t, "nested\n", files["./subdir/nested.txt"])
		})
	}
}

// TestRootfsOCIUnikraftImageFormatMismatch asserts that a format the initrd
// cannot satisfy is reported rather than silently ignored.
func TestRootfsOCIUnikraftImageFormatMismatch(t *testing.T) {
	srcDir := writeTestDirectory(t)
	archivePath := writeUnikraftOCIArchive(t, srcDir)

	_, err := BuildRootfs(builderTestContext(t), BuildOpts{
		Rootfs: FSOpts{
			Path:   "oci-archive://" + archivePath,
			Type:   kraftfile.SourceTypeOCI,
			Format: kraftfile.FsTypeErofs,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.ErrorContains(t, err, "rootfs format mismatch")
}

// TestRootfsOCIUnikraftImageConfig asserts that the source image's config
// survives, and that the build options still override it.
func TestRootfsOCIUnikraftImageConfig(t *testing.T) {
	srcDir := writeTestDirectory(t)
	archivePath := writeUnikraftOCIArchive(t, srcDir, imagespec.WithImageConfig(ocispec.ImageConfig{
		Cmd: []string{"/from-image"},
		Env: []string{"FROM_IMAGE=1", "SHADOWED=image"},
	}))

	t.Run("inherited", func(t *testing.T) {
		imgs := runBuildRootfs(t, BuildOpts{
			Rootfs: FSOpts{
				Path: "oci-archive://" + archivePath,
				Type: kraftfile.SourceTypeOCI,
			},
			Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
		})
		require.Len(t, imgs, 1)
		require.Equal(t, []string{"/from-image"}, imgs[0].Image.Config.Cmd)
		require.Contains(t, imgs[0].Image.Config.Env, "FROM_IMAGE=1")
	})

	t.Run("overridden", func(t *testing.T) {
		imgs := runBuildRootfs(t, BuildOpts{
			Rootfs: FSOpts{
				Path: "oci-archive://" + archivePath,
				Type: kraftfile.SourceTypeOCI,
			},
			Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
			Cmd:      []string{"/from-opts"},
			Env:      kraftfile.Map{{Key: "SHADOWED", Value: "opts"}},
		})
		require.Len(t, imgs, 1)
		require.Equal(t, []string{"/from-opts"}, imgs[0].Image.Config.Cmd)
		require.Equal(t, []string{"FROM_IMAGE=1", "SHADOWED=opts"},
			imgs[0].Image.Config.Env)
	})
}

// TestRomOCIUnikraftImagePadded covers the path BuildRoms takes: a ROM must be
// page-aligned or the platform rejects it.
func TestRomOCIUnikraftImagePadded(t *testing.T) {
	srcDir := writeTestDirectory(t)
	archivePath := writeUnikraftOCIArchive(t, srcDir)

	roms, err := BuildRoms(builderTestContext(t), BuildOpts{
		Roms: []FSOpts{{
			Path:   "oci-archive://" + archivePath,
			Type:   kraftfile.SourceTypeOCI,
			Format: kraftfile.FsTypeCpio,
			Pad:    4096,
		}},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.NoError(t, err)
	require.Len(t, roms, 1)
	require.Len(t, roms[0], 1)
	t.Cleanup(func() { _ = roms[0][0].Cleanup() })

	_, size, err := roms[0][0].Open(t.Context())
	require.NoError(t, err)
	require.NotZero(t, size)
	require.Zero(t, size%4096, "rom must be padded to page alignment")
}

// TestRootfsOCISinglePlatformImageMultiplePlatforms verifies that a
// single-platform image is not silently reused for every requested platform.
func TestRootfsOCISinglePlatformImageMultiplePlatforms(t *testing.T) {
	srcDir := writeTestDirectory(t)
	archivePath := writeUnikraftOCIArchive(t, srcDir)

	_, err := BuildRootfs(builderTestContext(t), BuildOpts{
		Rootfs: FSOpts{
			Path: "oci-archive://" + archivePath,
			Type: kraftfile.SourceTypeOCI,
		},
		Platform: []ocispec.Platform{
			{OS: "fc", Architecture: "x86_64"},
			{OS: "fc", Architecture: "arm64"},
		},
	})
	require.ErrorContains(t, err, "does not contain platform")
}

func TestRootfsOCIRegularImageNonRegistry(t *testing.T) {
	// Without an initrd component the image is a regular one, which only
	// BuildKit can flatten, and that needs a registry to pull from.
	archivePath := filepath.Join(t.TempDir(), "regular-image.tar")
	img := imagespec.NewImage(imagespec.WithPlatform(ocispec.Platform{OS: "linux", Architecture: "amd64"}))
	require.NoError(t, imagespec.SaveTarball(t.Context(), archivePath, img))

	_, err := BuildRootfs(builderTestContext(t), BuildOpts{
		Rootfs: FSOpts{
			Path:   "oci-archive://" + archivePath,
			Type:   kraftfile.SourceTypeOCI,
			Format: kraftfile.FsTypeCpio,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.ErrorContains(t, err, "must be a registry reference")
}

// TestRootfsOCIRegularImageIntegration reads a regular OCI image (plain layers,
// no unikraft components) from a registry and verifies that BuildKit flattens
// the layers and that the result is re-packaged into the requested rootfs
// format.
func TestRootfsOCIRegularImageIntegration(t *testing.T) {
	ctx := rootfsIntegrationContext(t)
	ref := integration.HelloWorld.Mirror(t, ctx)

	for _, format := range []kraftfile.FsType{kraftfile.FsTypeCpio, kraftfile.FsTypeErofs} {
		t.Run(string(format), func(t *testing.T) {
			imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
				Rootfs: FSOpts{
					Path:   ref,
					Type:   kraftfile.SourceTypeOCI,
					Format: format,
				},
				Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
			})
			require.Len(t, imgs, 1)

			switch format {
			case kraftfile.FsTypeCpio:
				files := readCpioInitrd(t, imgs[0])
				require.Contains(t, files, "./hello")
			case kraftfile.FsTypeErofs:
				files := readErofsInitrd(t, imgs[0])
				require.Contains(t, files, "hello")
			}
		})
	}
}

// TestRootfsOCIRegularImageNoCacheIntegration guards the options that carry
// --no-cache into a raw LLB solve, which does not see the frontend attribute the
// Dockerfile path uses.
func TestRootfsOCIRegularImageNoCacheIntegration(t *testing.T) {
	ctx := rootfsIntegrationContext(t)
	imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
		Rootfs: FSOpts{
			Path:   integration.HelloWorld.Mirror(t, ctx),
			Type:   kraftfile.SourceTypeOCI,
			Format: kraftfile.FsTypeCpio,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
		NoCache:  true,
	})
	require.Len(t, imgs, 1)
	require.Contains(t, readCpioInitrd(t, imgs[0]), "./hello")
}

// TestRootfsOCIRegularImagePerArchIntegration covers two platforms that resolve
// to different manifests of one multi-arch image: each must be flattened on its
// own rather than sharing the first one's filesystem.
func TestRootfsOCIRegularImagePerArchIntegration(t *testing.T) {
	ctx := rootfsIntegrationContext(t)
	imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
		Rootfs: FSOpts{
			Path:   integration.HelloWorld.Mirror(t, ctx),
			Type:   kraftfile.SourceTypeOCI,
			Format: kraftfile.FsTypeCpio,
		},
		Platform: []ocispec.Platform{
			{OS: "fc", Architecture: "x86_64"},
			{OS: "fc", Architecture: "arm64"},
		},
	})
	require.Len(t, imgs, 2)
	require.NotEqual(t, readCpioInitrd(t, imgs[0]), readCpioInitrd(t, imgs[1]),
		"each architecture must get its own flattened filesystem")
	assertPlatforms(t, imgs, []string{"fc/x86_64", "fc/arm64"})
}

// TestRootfsOCIRegularImageSharedFlattenIntegration covers two unikraft
// platforms that normalise onto the same linux platform: they share one source
// image, so the flatten is done once and reused rather than solved per platform.
func TestRootfsOCIRegularImageSharedFlattenIntegration(t *testing.T) {
	ctx := rootfsIntegrationContext(t)
	imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
		Rootfs: FSOpts{
			Path:   integration.HelloWorld.Mirror(t, ctx),
			Type:   kraftfile.SourceTypeOCI,
			Format: kraftfile.FsTypeCpio,
		},
		Platform: []ocispec.Platform{
			{OS: "fc", Architecture: "x86_64"},
			{OS: "qemu", Architecture: "x86_64"},
		},
	})
	require.Len(t, imgs, 2)
	require.Equal(t, readCpioInitrd(t, imgs[0]), readCpioInitrd(t, imgs[1]))
	assertPlatforms(t, imgs, []string{"fc/x86_64", "qemu/x86_64"})
}
