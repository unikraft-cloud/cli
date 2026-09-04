// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	goerofs "github.com/unikraft/go-archivefs/erofs"
	"github.com/unikraft/go-cpio"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/builder/buildfs"
	"unikraft.com/cli/internal/buildkit"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/integration"
	"unikraft.com/x/kraftfile"
)

func TestDetectSourceTypeEmpty(t *testing.T) {
	_, err := DetectSourceType("")
	require.ErrorContains(t, err, "empty rootfs path")
}

func TestDetectSourceTypeNonexistent(t *testing.T) {
	_, err := DetectSourceType(filepath.Join(t.TempDir(), "nonexistent"))
	require.ErrorContains(t, err, "rootfs path does not exist")
}

func TestDetectSourceTypeDockerfile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Dockerfile")
	require.NoError(t, os.WriteFile(p, []byte("FROM scratch\n"), 0o644))

	typ, err := DetectSourceType(p)
	require.NoError(t, err)
	require.Equal(t, kraftfile.SourceTypeDockerfile, typ)
}

func TestDetectSourceTypeDockerfileWithPrefix(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "foo.Dockerfile")
	require.NoError(t, os.WriteFile(p, []byte("FROM scratch\n"), 0o644))

	typ, err := DetectSourceType(p)
	require.NoError(t, err)
	require.Equal(t, kraftfile.SourceTypeDockerfile, typ)
}

func TestDetectSourceTypeDirectory(t *testing.T) {
	dir := writeTestDirectory(t)

	typ, err := DetectSourceType(dir)
	require.NoError(t, err)
	require.Equal(t, kraftfile.SourceTypeDirectory, typ)
}

func TestDetectSourceTypeCpio(t *testing.T) {
	cpioPath := writeTestCpioFile(t)

	typ, err := DetectSourceType(cpioPath)
	require.NoError(t, err)
	require.Equal(t, kraftfile.SourceTypeCpio, typ)
}

func TestDetectSourceTypeErofs(t *testing.T) {
	erofsPath := writeTestErofsFile(t)

	typ, err := DetectSourceType(erofsPath)
	require.NoError(t, err)
	require.Equal(t, kraftfile.SourceTypeErofs, typ)
}

func TestDetectSourceTypeTarball(t *testing.T) {
	tarPath := writeTestTarballFile(t)

	typ, err := DetectSourceType(tarPath)
	require.NoError(t, err)
	require.Equal(t, kraftfile.SourceTypeTarball, typ)
}

func TestDetectSourceTypeUnknown(t *testing.T) {
	p := filepath.Join(t.TempDir(), "random.bin")
	require.NoError(t, os.WriteFile(p, []byte("not an archive"), 0o644))

	_, err := DetectSourceType(p)
	require.ErrorContains(t, err, "could not detect file rootfs type")
}

func TestRootfsCpioArchive(t *testing.T) {
	cpioPath := writeTestCpioFile(t)

	imgs := runBuildRootfs(t, BuildOpts{
		Rootfs: FSOpts{
			Path: cpioPath,
			Type: kraftfile.SourceTypeCpio,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)

	// The pre-built CPIO is passed through as-is; verify its contents.
	files := readCpioInitrd(t, imgs[0])
	require.Contains(t, files, "./hello.txt")
	require.Equal(t, "hello\n", files["./hello.txt"])
	require.Contains(t, files, "./subdir/nested.txt")
	require.Equal(t, "nested\n", files["./subdir/nested.txt"])
}

func TestRootfsErofsArchive(t *testing.T) {
	erofsPath := writeTestErofsFile(t)

	imgs := runBuildRootfs(t, BuildOpts{
		Rootfs: FSOpts{
			Path: erofsPath,
			Type: kraftfile.SourceTypeErofs,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)

	// The pre-built EROFS is passed through as-is; verify its contents.
	files := readErofsInitrd(t, imgs[0])
	require.Contains(t, files, "hello.txt")
	require.Equal(t, "hello\n", files["hello.txt"])
	require.Contains(t, files, "subdir/nested.txt")
	require.Equal(t, "nested\n", files["subdir/nested.txt"])
}

func TestRootfsDirectory(t *testing.T) {
	dir := writeTestDirectory(t)

	imgs := runBuildRootfs(t, BuildOpts{
		Rootfs: FSOpts{
			Path:   dir,
			Type:   kraftfile.SourceTypeDirectory,
			Format: kraftfile.FsTypeErofs,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)

	// Directory was packaged into EROFS; verify its contents.
	files := readErofsInitrd(t, imgs[0])
	require.Contains(t, files, "hello.txt")
	require.Equal(t, "hello\n", files["hello.txt"])
	require.Contains(t, files, "subdir/nested.txt")
	require.Equal(t, "nested\n", files["subdir/nested.txt"])
}

func TestRootfsDirectoryCpioFormat(t *testing.T) {
	dir := writeTestDirectory(t)

	imgs := runBuildRootfs(t, BuildOpts{
		Rootfs: FSOpts{
			Path:   dir,
			Type:   kraftfile.SourceTypeDirectory,
			Format: kraftfile.FsTypeCpio,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)

	// Directory was packaged into CPIO; verify its contents.
	files := readCpioInitrd(t, imgs[0])
	require.Contains(t, files, "./hello.txt")
	require.Equal(t, "hello\n", files["./hello.txt"])
	require.Contains(t, files, "./subdir/nested.txt")
	require.Equal(t, "nested\n", files["./subdir/nested.txt"])
}

func TestRootfsTarball(t *testing.T) {
	tarPath := writeTestTarballFile(t)

	for _, format := range []kraftfile.FsType{kraftfile.FsTypeCpio, kraftfile.FsTypeErofs} {
		t.Run(string(format), func(t *testing.T) {
			imgs := runBuildRootfs(t, BuildOpts{
				Rootfs: FSOpts{
					Path:   tarPath,
					Type:   kraftfile.SourceTypeTarball,
					Format: format,
				},
				Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
			})
			require.Len(t, imgs, 1)

			switch format {
			case kraftfile.FsTypeCpio:
				files := readCpioInitrd(t, imgs[0])
				require.Contains(t, files, "./hello.txt")
				require.Equal(t, "hello\n", files["./hello.txt"])
				require.Contains(t, files, "./subdir/nested.txt")
				require.Equal(t, "nested\n", files["./subdir/nested.txt"])
			case kraftfile.FsTypeErofs:
				files := readErofsInitrd(t, imgs[0])
				require.Contains(t, files, "hello.txt")
				require.Equal(t, "hello\n", files["hello.txt"])
				require.Contains(t, files, "subdir/nested.txt")
				require.Equal(t, "nested\n", files["subdir/nested.txt"])
			}
		})
	}
}

func TestRootfsDockerfileCpioIntegration(t *testing.T) {
	ctx := rootfsIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeDockerfile(t, dockerfile)
	imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Path:   rootfsPath,
			Type:   kraftfile.SourceTypeDockerfile,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)

	files := readCpioInitrd(t, imgs[0])
	require.Contains(t, files, "./hello.txt")
	require.Equal(t, "hello\n", files["./hello.txt"])
}

func TestRootfsDockerfileErofsIntegration(t *testing.T) {
	ctx := rootfsIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeDockerfile(t, dockerfile)
	imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeErofs,
			Path:   rootfsPath,
			Type:   kraftfile.SourceTypeDockerfile,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)

	files := readErofsInitrd(t, imgs[0])
	require.Contains(t, files, "hello.txt")
	require.Equal(t, "hello\n", files["hello.txt"])
}

func TestRootfsDockerfileCustomNameIntegration(t *testing.T) {
	ctx := rootfsIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	contextDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "MyDockerfile"), []byte(dockerfile), 0o644))

	imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
		Rootfs: FSOpts{
			Format:     kraftfile.FsTypeCpio,
			Path:       contextDir,
			Dockerfile: "MyDockerfile",
			Type:       kraftfile.SourceTypeDockerfile,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)

	files := readCpioInitrd(t, imgs[0])
	require.Contains(t, files, "./hello.txt")
	require.Equal(t, "hello\n", files["./hello.txt"])
}

func TestRootfsDockerfileWithDeviceNodeIntegration(t *testing.T) {
	ctx := rootfsIntegrationContext(t)
	// Build an image that contains a device node. The local exporter
	// used during solve must not fail with "failed to create device"
	// when unpacking the result. See TOOL-791.
	dockerfile := `
FROM busybox:latest
RUN mknod /testdevice c 1 3

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeDockerfile(t, dockerfile)

	for _, format := range []kraftfile.FsType{kraftfile.FsTypeCpio, kraftfile.FsTypeErofs} {
		t.Run(string(format), func(t *testing.T) {
			imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
				Rootfs: FSOpts{
					Format: format,
					Path:   rootfsPath,
					Type:   kraftfile.SourceTypeDockerfile,
				},
				Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
			})
			require.Len(t, imgs, 1)

			switch format {
			case kraftfile.FsTypeCpio:
				files := readCpioInitrd(t, imgs[0])
				require.Contains(t, files, "./hello.txt")
				require.Equal(t, "hello\n", files["./hello.txt"])
			case kraftfile.FsTypeErofs:
				files := readErofsInitrd(t, imgs[0])
				require.Contains(t, files, "hello.txt")
				require.Equal(t, "hello\n", files["hello.txt"])
			}
		})
	}
}

func TestRootfsMultiPlatform(t *testing.T) {
	ctx := rootfsIntegrationContext(t)
	// $BUILDPLATFORM avoids emulation; TARGETARCH still varies per platform.
	dockerfile := `
FROM --platform=$BUILDPLATFORM busybox AS base
ARG TARGETARCH
RUN echo "$TARGETARCH" > /arch.txt

FROM scratch
COPY --from=base /arch.txt /arch.txt
`
	rootfsPath := writeDockerfile(t, dockerfile)
	imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Path:   rootfsPath,
			Type:   kraftfile.SourceTypeDockerfile,
		},
		Platform: []ocispec.Platform{
			{OS: "fc", Architecture: "x86_64"},
			{OS: "fc", Architecture: "arm64"},
		},
	})
	require.Len(t, imgs, 2)

	// Each platform must produce distinct content, proving they were
	// solved and exported independently rather than merged into one export.
	content := make(map[string]string)
	for _, img := range imgs {
		files := readCpioInitrd(t, img)
		require.Contains(t, files, "./arch.txt")
		content[platforms.Format(img.Image.Platform)] = files["./arch.txt"]
	}
	require.Equal(t, "amd64\n", content["fc/x86_64"])
	require.Equal(t, "arm64\n", content["fc/arm64"])
}

func TestRootfsMultiPlatformEmulation(t *testing.T) {
	t.Skip("QEMU emulation is not reliably available across CI runners")

	ctx := rootfsIntegrationContext(t)
	// Unlike above, RUN executes under the target platform here, requiring
	// QEMU emulation rather than just varying TARGETARCH.
	dockerfile := `
FROM busybox AS base
RUN uname -m > /arch.txt

FROM scratch
COPY --from=base /arch.txt /arch.txt
`
	rootfsPath := writeDockerfile(t, dockerfile)
	imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Path:   rootfsPath,
			Type:   kraftfile.SourceTypeDockerfile,
		},
		Platform: []ocispec.Platform{
			{OS: "fc", Architecture: "x86_64"},
			{OS: "fc", Architecture: "arm64"},
		},
	})
	require.Len(t, imgs, 2)

	content := make(map[string]string)
	for _, img := range imgs {
		files := readCpioInitrd(t, img)
		require.Contains(t, files, "./arch.txt")
		content[platforms.Format(img.Image.Platform)] = files["./arch.txt"]
	}
	require.Equal(t, "x86_64\n", content["fc/x86_64"])
	require.Equal(t, "aarch64\n", content["fc/arm64"])
}

func TestRootfsNoPlatform(t *testing.T) {
	cpioPath := writeTestCpioFile(t)
	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.InfoLevel))

	_, err := BuildRootfs(ctx, BuildOpts{
		Rootfs: FSOpts{
			Path: cpioPath,
			Type: kraftfile.SourceTypeCpio,
		},
	})
	require.ErrorContains(t, err, "at least one platform must be specified")
}

func TestRootfsPreservesConfig(t *testing.T) {
	dir := writeTestDirectory(t)

	imgs := runBuildRootfs(t, BuildOpts{
		Rootfs: FSOpts{
			Path:   dir,
			Type:   kraftfile.SourceTypeDirectory,
			Format: kraftfile.FsTypeCpio,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
		Cmd:      []string{"/bin/app", "--flag"},
		Env: kraftfile.Map{
			{Key: "FOO", Value: "bar"},
		},
		Labels: map[string]string{"test": "value"},
	})
	require.Len(t, imgs, 1)
	require.NotNil(t, imgs[0])
	require.NotNil(t, imgs[0].Image)
	require.Equal(t, []string{"/bin/app", "--flag"}, imgs[0].Image.Config.Cmd)
	require.Contains(t, imgs[0].Image.Config.Env, "FOO=bar")
	require.Equal(t, map[string]string{"test": "value"}, imgs[0].Image.Config.Labels)

	// Config assertions above are sufficient, but also verify the initrd is valid.
	files := readCpioInitrd(t, imgs[0])
	require.Contains(t, files, "./hello.txt")
}

func TestRootfsDirectoryDefaultFormat(t *testing.T) {
	dir := writeTestDirectory(t)

	imgs := runBuildRootfs(t, BuildOpts{
		Rootfs: FSOpts{
			Path: dir,
			Type: kraftfile.SourceTypeDirectory,
			// Format intentionally omitted; should default to cpio.
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)

	// Default format is CPIO.
	files := readCpioInitrd(t, imgs[0])
	require.Contains(t, files, "./hello.txt")
	require.Equal(t, "hello\n", files["./hello.txt"])
	require.Contains(t, files, "./subdir/nested.txt")
	require.Equal(t, "nested\n", files["./subdir/nested.txt"])
}

func TestRomDefaultFormat(t *testing.T) {
	dir := writeTestDirectory(t)

	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.InfoLevel))

	romFiles, err := BuildRoms(ctx, BuildOpts{
		Roms: []FSOpts{
			{
				Path: dir,
				Type: kraftfile.SourceTypeDirectory,
				// Format intentionally omitted; should default to erofs.
			},
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.NoError(t, err)
	require.Len(t, romFiles, 1)
	require.Len(t, romFiles[0], 1) // one platform: the default
	t.Cleanup(func() { _ = romFiles[0][0].Cleanup() })

	// Verify the rom was built as EROFS by reading it back.
	rc, _, err := romFiles[0][0].Open(ctx)
	require.NoError(t, err)
	defer rc.Close()

	tmp, err := os.CreateTemp(t.TempDir(), "rom-*.img")
	require.NoError(t, err)
	defer tmp.Close()

	_, err = io.Copy(tmp, rc)
	require.NoError(t, err)

	fsys, err := goerofs.Open(tmp)
	require.NoError(t, err)

	files := make(map[string]string)
	readErofsDir(t, fsys, ".", files)
	require.Contains(t, files, "hello.txt")
	require.Equal(t, "hello\n", files["hello.txt"])
	require.Contains(t, files, "subdir/nested.txt")
	require.Equal(t, "nested\n", files["subdir/nested.txt"])
}

// A ROM payload is a compiled artifact, so a multi-platform build must produce
// one ROM per platform rather than reusing a single one everywhere. Reusing it
// silently ships, say, an x86_64 binary inside the arm64 image.
func TestRomPerPlatform(t *testing.T) {
	dir := writeTestDirectory(t)

	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.InfoLevel))

	plats := []ocispec.Platform{
		{Architecture: "amd64", OS: "linux"},
		{Architecture: "arm64", OS: "linux"},
	}

	romFiles, err := BuildRoms(ctx, BuildOpts{
		Roms: []FSOpts{
			{
				Path: dir,
				Type: kraftfile.SourceTypeDirectory,
			},
		},
		Platform: plats,
	})
	require.NoError(t, err)
	require.Len(t, romFiles, 1)
	require.Len(t, romFiles[0], len(plats), "one rom per platform")

	t.Cleanup(func() {
		for _, f := range romFiles[0] {
			_ = f.Cleanup()
		}
	})

	// Each platform must own a distinct artifact. Sharing a single handle
	// across platforms is the bug this guards against: with a Dockerfile
	// source the payload is compiled per $TARGETARCH, so reusing one would
	// put the wrong architecture's binary in every image but the first.
	//
	// The bytes are legitimately identical here -- a directory source does
	// not vary by platform -- so the assertion is on the handles, not on
	// their contents.
	for i, f := range romFiles[0] {
		require.NotNil(t, f, "no rom for platform %d", i)
	}
	require.NotSame(t, romFiles[0][0], romFiles[0][1])
}

func TestRootfsUnsupportedType(t *testing.T) {
	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.InfoLevel))

	_, err := BuildRootfs(ctx, BuildOpts{
		Rootfs: FSOpts{
			Path: "/some/path",
			Type: kraftfile.SourceType("unsupported"),
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.ErrorContains(t, err, "unsupported rootfs type")
}

func TestRootfsCpioSourceErofsFormatMismatch(t *testing.T) {
	cpioPath := writeTestCpioFile(t)
	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.InfoLevel))

	_, err := BuildRootfs(ctx, BuildOpts{
		Rootfs: FSOpts{
			Path:   cpioPath,
			Type:   kraftfile.SourceTypeCpio,
			Format: kraftfile.FsTypeErofs,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.ErrorContains(t, err, "rootfs format mismatch")
}

func TestRootfsErofsSourceCpioFormatMismatch(t *testing.T) {
	erofsPath := writeTestErofsFile(t)
	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.InfoLevel))

	_, err := BuildRootfs(ctx, BuildOpts{
		Rootfs: FSOpts{
			Path:   erofsPath,
			Type:   kraftfile.SourceTypeErofs,
			Format: kraftfile.FsTypeCpio,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.ErrorContains(t, err, "rootfs format mismatch")
}

func rootfsIntegrationContext(t *testing.T) context.Context {
	t.Helper()
	integration.SkipUnlessIntegration(t)
	t.Setenv("BUILDKIT_PROGRESS", "quiet")

	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.InfoLevel))
	cfg, err := integration.LoadConfig(t)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("integration config not found")
	}
	require.NoError(t, err)
	if cfg == nil || cfg.Config == nil {
		t.Skip("integration config unavailable")
	}
	ctx = config.WithConfig(ctx, cfg.Config)
	return ctx
}

// runBuildRootfsIntegration calls BuildRootfs with a BuildKit-enabled context.
func runBuildRootfsIntegration(t *testing.T, ctx context.Context, opts BuildOpts) []*imagespec.Image {
	t.Helper()
	bkc, cleanup, err := buildkit.ConnectToBuildkit(ctx)
	if err != nil {
		t.Skipf("buildkit unavailable: %v", err)
	}
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	ctx = buildkit.WithBuildkitContext(ctx, bkc)

	imgs, err := BuildRootfs(ctx, opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, img := range imgs {
			_ = img.Close()
		}
	})
	return imgs
}

// readCpioInitrd opens the initrd from img and returns a map of file name to
// content for all regular files in the CPIO archive.
func readCpioInitrd(t *testing.T, img *imagespec.Image) map[string]string {
	t.Helper()
	require.NotNil(t, img.Initrd)

	ctx := t.Context()
	f, _, err := img.Initrd.Open(ctx)
	require.NoError(t, err)
	defer f.Close()

	cr := cpio.NewReader(f)
	files := make(map[string]string)
	for {
		hdr, err := cr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)

		if hdr.Mode&cpio.TypeRegular != 0 {
			data, err := io.ReadAll(cr)
			require.NoError(t, err)
			files[hdr.Name] = string(data)
		}
	}
	return files
}

// readErofsInitrd opens the initrd from img and returns a map of file name to
// content for all regular files in the EROFS archive.
func readErofsInitrd(t *testing.T, img *imagespec.Image) map[string]string {
	t.Helper()
	require.NotNil(t, img.Initrd)

	ctx := t.Context()
	rc, _, err := img.Initrd.Open(ctx)
	require.NoError(t, err)
	defer rc.Close()

	// EROFS needs io.ReaderAt; copy the stream into a temporary file.
	tmp, err := os.CreateTemp(t.TempDir(), "erofs-*.img")
	require.NoError(t, err)
	defer tmp.Close()

	_, err = io.Copy(tmp, rc)
	require.NoError(t, err)

	fsys, err := goerofs.Open(tmp)
	require.NoError(t, err)

	files := make(map[string]string)
	readErofsDir(t, fsys, ".", files)
	return files
}

// readErofsDir recursively reads all regular files from an EROFS filesystem.
func readErofsDir(t *testing.T, fsys *goerofs.Filesystem, dir string, files map[string]string) {
	t.Helper()
	entries, err := fsys.ReadDir(dir)
	require.NoError(t, err)

	for _, entry := range entries {
		name := dir + "/" + entry.Name()
		if dir == "." {
			name = entry.Name()
		}
		if entry.IsDir() {
			readErofsDir(t, fsys, name, files)
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		f, err := fsys.Open(name)
		require.NoError(t, err)
		data, err := io.ReadAll(f)
		f.Close()
		require.NoError(t, err)
		files[name] = string(data)
	}
}

// writeTestDirectory creates a temporary directory with some test files.
func writeTestDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "subdir", "nested.txt"), []byte("nested\n"), 0o644))
	return dir
}

// writeTestCpioFile creates a valid CPIO archive from a temporary directory.
func writeTestCpioFile(t *testing.T) string {
	t.Helper()
	srcDir := writeTestDirectory(t)

	cpioPath := filepath.Join(t.TempDir(), "test.cpio")
	f, err := os.Create(cpioPath)
	require.NoError(t, err)
	defer f.Close()

	ctx := context.Background()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.InfoLevel))
	require.NoError(t, buildfs.CreateCPIO(ctx, f, os.DirFS(srcDir)))
	return cpioPath
}

// writeTestErofsFile creates a valid EROFS archive from a temporary directory.
func writeTestErofsFile(t *testing.T) string {
	t.Helper()
	srcDir := writeTestDirectory(t)

	erofsPath := filepath.Join(t.TempDir(), "test.erofs")
	f, err := os.Create(erofsPath)
	require.NoError(t, err)
	defer f.Close()

	require.NoError(t, buildfs.CreateEROFS(f, os.DirFS(srcDir)))
	return erofsPath
}

// writeTestTarballFile creates a valid tarball from a temporary directory.
func writeTestTarballFile(t *testing.T) string {
	t.Helper()
	srcDir := writeTestDirectory(t)

	tarPath := filepath.Join(t.TempDir(), "test.tar")
	f, err := os.Create(tarPath)
	require.NoError(t, err)
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
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

	return tarPath
}

// runBuildRootfs calls BuildRootfs and registers cleanup for the returned images.
func runBuildRootfs(t *testing.T, opts BuildOpts) []*imagespec.Image {
	t.Helper()
	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.InfoLevel))

	imgs, err := BuildRootfs(ctx, opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, img := range imgs {
			_ = img.Close()
		}
	})
	return imgs
}

func TestRootfsDockerfileBuildContextIntegration(t *testing.T) {
	ctx := rootfsIntegrationContext(t)

	contextDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(contextDir, "hello.txt"),
		[]byte("hello from context\n"),
		0o644,
	))

	dockerfile := `
FROM scratch
COPY --from=shared hello.txt /hello.txt
`

	rootfsPath := writeDockerfile(t, dockerfile)

	imgs := runBuildRootfsIntegration(t, ctx, BuildOpts{
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Path:   rootfsPath,
			Type:   kraftfile.SourceTypeDockerfile,
		},
		BuildContexts: map[string]string{
			"shared": contextDir,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})

	require.Len(t, imgs, 1)

	files := readCpioInitrd(t, imgs[0])
	require.Contains(t, files, "./hello.txt")
	require.Equal(t, "hello from context\n", files["./hello.txt"])
}
