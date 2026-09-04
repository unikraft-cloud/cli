// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"archive/tar"
	"bytes"
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
	return writeOCIArchive(t, "regular-image.tar",
		[]ocispec.Descriptor{layerDesc}, [][]byte{layerBlob}, []digest.Digest{diffID})
}

// writeOCIArchive wraps the given layers in a regular OCI image and saves it as
// an OCI archive tarball named name. diffIDs are the digests of the uncompressed
// layers, which is what the config records, as opposed to the descriptors'
// digests of the compressed blobs. It returns the path to the tarball.
func writeOCIArchive(t *testing.T, name string, layerDescs []ocispec.Descriptor, layerBlobs [][]byte, diffIDs []digest.Digest) string {
	t.Helper()
	require.Len(t, diffIDs, len(layerDescs))

	config := ocispec.Image{
		Architecture: "amd64",
		OS:           "linux",
		Config: ocispec.ImageConfig{
			Cmd: []string{"/bin/sh"},
		},
		RootFS: ocispec.RootFS{
			Type:    "layers",
			DiffIDs: diffIDs,
		},
	}
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	configDesc, configBlob := newDescriptor("application/vnd.oci.image.config.v1+json", configJSON)

	manifest := ocispec.Manifest{
		SchemaVersion: 2,
		MediaType:     ocispec.MediaTypeImageManifest,
		Config:        configDesc,
		Layers:        layerDescs,
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

	archivePath := filepath.Join(t.TempDir(), name)
	out, err := os.Create(archivePath)
	require.NoError(t, err)
	defer out.Close()

	tw := tar.NewWriter(out)
	defer tw.Close()

	writeTarEntry(t, tw, "blobs/sha256/"+configDesc.Digest.Encoded(), configBlob)
	writeTarEntry(t, tw, "blobs/sha256/"+manifestDesc.Digest.Encoded(), manifestBlob)
	for i, desc := range layerDescs {
		writeTarEntry(t, tw, "blobs/sha256/"+desc.Digest.Encoded(), layerBlobs[i])
	}
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
	// Hash the uncompressed stream as it is written, so that the diffID does not
	// require keeping a second copy of the layer around.
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
