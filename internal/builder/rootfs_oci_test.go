// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"archive/tar"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/cli/internal/builder/buildfs"
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

// writeRegularOCIArchive builds a regular OCI image (plain OCI layers, no
// unikraft components) from srcDir and saves it as an OCI archive tarball.
// It returns the path to the tarball.
func writeRegularOCIArchive(t *testing.T, srcDir string) string {
	t.Helper()

	layerDesc, layerBlob, diffID := tarGzipLayer(t, srcDir)

	config := ocispec.Image{
		Architecture: "amd64",
		OS:           "linux",
		Config: ocispec.ImageConfig{
			Cmd: []string{"/bin/sh"},
		},
		RootFS: ocispec.RootFS{
			Type:    "layers",
			DiffIDs: []digest.Digest{diffID},
		},
	}
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	configDesc, configBlob := newDescriptor("application/vnd.oci.image.config.v1+json", configJSON)

	manifest := ocispec.Manifest{
		SchemaVersion: 2,
		MediaType:     ocispec.MediaTypeImageManifest,
		Config:        configDesc,
		Layers:        []ocispec.Descriptor{layerDesc},
	}
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestDesc, manifestBlob := newDescriptor(ocispec.MediaTypeImageManifest, manifestJSON)

	// A regular OCI image advertises a real platform, not a unikraft one. The
	// builder matches the requested unikraft target against its normalised
	// linux equivalent.
	index := ocispec.Index{
		SchemaVersion: 2,
		MediaType:     ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    manifestDesc.Digest,
			Size:      manifestDesc.Size,
			Platform: &ocispec.Platform{
				Architecture: "amd64",
				OS:           "linux",
			},
		}},
	}
	indexJSON, err := json.Marshal(index)
	require.NoError(t, err)

	archivePath := filepath.Join(t.TempDir(), "regular-image.tar")
	out, err := os.Create(archivePath)
	require.NoError(t, err)
	defer out.Close()

	tw := tar.NewWriter(out)
	defer tw.Close()

	writeTarEntry(t, tw, "blobs/sha256/"+configDesc.Digest.Encoded(), configBlob)
	writeTarEntry(t, tw, "blobs/sha256/"+manifestDesc.Digest.Encoded(), manifestBlob)
	writeTarEntry(t, tw, "blobs/sha256/"+layerDesc.Digest.Encoded(), layerBlob)
	writeTarEntry(t, tw, "index.json", indexJSON)
	writeTarEntry(t, tw, "oci-layout", []byte(`{"imageLayoutVersion":"1.0.0"}`))

	return archivePath
}

// newDescriptor computes the sha256 digest and size of data and returns a
// descriptor with the given media type plus the raw bytes.
func newDescriptor(mediaType string, data []byte) (ocispec.Descriptor, []byte) {
	return ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}, data
}

// tarGzipLayer walks srcDir and returns a gzip-compressed tar layer descriptor,
// its raw blob bytes, and the diffID, which is the digest of the tar stream
// before compression.
func tarGzipLayer(t *testing.T, srcDir string) (ocispec.Descriptor, []byte, digest.Digest) {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	diffIDer := digest.SHA256.Digester()
	tw := tar.NewWriter(io.MultiWriter(gw, diffIDer.Hash()))

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	blob := buf.Bytes()
	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayerGzip,
		Digest:    digest.FromBytes(blob),
		Size:      int64(len(blob)),
	}
	return desc, blob, diffIDer.Digest()
}

func writeTarEntry(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(data)),
	}))
	_, err := io.Copy(tw, bytes.NewReader(data))
	require.NoError(t, err)
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
	srcDir := writeTestDirectory(t)
	archivePath := writeRegularOCIArchive(t, srcDir)

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
// regularImageRef is a small multi-arch image of plain OCI layers, which is
// what the flatten path needs.
const regularImageRef = "index.docker.io/library/hello-world:latest"

func TestRootfsOCIRegularImageIntegration(t *testing.T) {
	for _, format := range []kraftfile.FsType{kraftfile.FsTypeCpio, kraftfile.FsTypeErofs} {
		t.Run(string(format), func(t *testing.T) {
			imgs := runBuildRootfsIntegration(t, rootfsIntegrationContext(t), BuildOpts{
				Rootfs: FSOpts{
					Path:   regularImageRef,
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
	imgs := runBuildRootfsIntegration(t, rootfsIntegrationContext(t), BuildOpts{
		Rootfs: FSOpts{
			Path:   regularImageRef,
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
	imgs := runBuildRootfsIntegration(t, rootfsIntegrationContext(t), BuildOpts{
		Rootfs: FSOpts{
			Path:   regularImageRef,
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
	imgs := runBuildRootfsIntegration(t, rootfsIntegrationContext(t), BuildOpts{
		Rootfs: FSOpts{
			Path:   regularImageRef,
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
