// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
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
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/builder/buildflags"
	"unikraft.com/cli/internal/buildkit"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/integration"
)

func TestDefaultRootfsFormatNoPlatforms(t *testing.T) {
	require.Equal(t, kraftfile.FsTypeCpio, DefaultRootfsFormat(nil))
}

func TestDefaultRootfsFormatAllErofsLibukfs(t *testing.T) {
	ps := []ocispec.Platform{
		{Architecture: "x86_64", OS: "fc", OSFeatures: []string{"CONFIG_LIBUKFS_EROFS=y"}},
		{Architecture: "arm64", OS: "fc", OSFeatures: []string{"CONFIG_LIBUKFS_EROFS=y"}},
	}
	require.Equal(t, kraftfile.FsTypeErofs, DefaultRootfsFormat(ps))
}

func TestDefaultRootfsFormatAllErofsFS(t *testing.T) {
	ps := []ocispec.Platform{
		{Architecture: "x86_64", OS: "fc", OSFeatures: []string{"CONFIG_EROFS_FS=y"}},
	}
	require.Equal(t, kraftfile.FsTypeErofs, DefaultRootfsFormat(ps))
}

func TestDefaultRootfsFormatMixedSupport(t *testing.T) {
	ps := []ocispec.Platform{
		{Architecture: "x86_64", OS: "fc", OSFeatures: []string{"CONFIG_LIBUKFS_EROFS=y"}},
		{Architecture: "arm64", OS: "fc", OSFeatures: []string{"CONFIG_OTHER=y"}},
	}
	require.Equal(t, kraftfile.FsTypeCpio, DefaultRootfsFormat(ps))
}

func TestDefaultRootfsFormatNoErofsFeatures(t *testing.T) {
	ps := []ocispec.Platform{
		{Architecture: "x86_64", OS: "fc", OSFeatures: []string{"CONFIG_OTHER=y"}},
	}
	require.Equal(t, kraftfile.FsTypeCpio, DefaultRootfsFormat(ps))
}

func TestDefaultRootfsFormatEmptyFeatures(t *testing.T) {
	ps := []ocispec.Platform{
		{Architecture: "x86_64", OS: "fc"},
	}
	require.Equal(t, kraftfile.FsTypeCpio, DefaultRootfsFormat(ps))
}

func TestDefaultRootfsFormatMixedErofsKeys(t *testing.T) {
	ps := []ocispec.Platform{
		{Architecture: "x86_64", OS: "fc", OSFeatures: []string{"CONFIG_LIBUKFS_EROFS=y"}},
		{Architecture: "arm64", OS: "fc", OSFeatures: []string{"CONFIG_EROFS_FS=y"}},
	}
	require.Equal(t, kraftfile.FsTypeErofs, DefaultRootfsFormat(ps))
}

func TestBuildSinglePlatformIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeDockerfile(t, dockerfile)
	opts := BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertPlatforms(t, imgs, []string{"kraftcloud/x86_64"})
}

func TestBuildSinglePlatformErofsIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeDockerfile(t, dockerfile)
	opts := BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeErofs,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertPlatforms(t, imgs, []string{"kraftcloud/x86_64"})
}

func TestBuildDefaultErofsFormatBaseCompatIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeDockerfile(t, dockerfile)
	opts := BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Type: kraftfile.SourceTypeDockerfile,
			Path: rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

	files := readErofsInitrd(t, imgs[0])
	require.Contains(t, files, "hello.txt")
	require.Equal(t, "hello\n", files["hello.txt"])
}

func TestBuildAbsoluteSymlinksIntegration(t *testing.T) {
	ctx := integrationContext(t)
	// Reproducer for https://github.com/moby/buildkit/issues/6684
	// This test ensures that absolute symlinks are correctly preserved in the rootfs
	// and not incorrectly rewritten with platform-specific prefixes.
	// The debian:bookworm-slim image contains absolute symlinks like /etc/alternatives/awk -> /usr/bin/mawk
	dockerfile := `
FROM debian:bookworm-slim

# Create test symlinks to verify correct behavior
RUN ln -s /usr/bin/bash /test-symlink
RUN ln -s /etc/passwd /another-test-link
`
	rootfsPath := writeDockerfile(t, dockerfile)
	opts := BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

	// Inspect the CPIO archive to verify symlink targets
	initrd := imgs[0].Initrd
	require.NotNil(t, initrd)

	f, _, err := initrd.Open(ctx)
	require.NoError(t, err)
	defer f.Close()

	cr := cpio.NewReader(f)

	// Track found symlinks and their targets
	foundSymlinks := make(map[string]string)

	for {
		hdr, err := cr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)

		if hdr.Mode&cpio.TypeSymlink != 0 {
			foundSymlinks[hdr.Name] = hdr.Linkname
		}
	}

	// Verify our test symlinks are correct
	require.Contains(t, foundSymlinks, "./test-symlink")
	require.Equal(t, "/usr/bin/bash", foundSymlinks["./test-symlink"],
		"test-symlink should point to /usr/bin/bash, not a rewritten path")

	require.Contains(t, foundSymlinks, "./another-test-link")
	require.Equal(t, "/etc/passwd", foundSymlinks["./another-test-link"],
		"another-test-link should point to /etc/passwd, not a rewritten path")

	// Verify a symlink from the debian image itself
	require.Contains(t, foundSymlinks, "./etc/alternatives/awk")
	require.Equal(t, "/usr/bin/mawk", foundSymlinks["./etc/alternatives/awk"],
		"Debian's awk symlink should point to /usr/bin/mawk, not a rewritten path")

	// Ensure no symlinks have been incorrectly rewritten with platform prefixes
	for name, target := range foundSymlinks {
		require.NotContains(t, target, "fc_x86_64",
			"symlink %s should not contain platform prefix in target: %s", name, target)
		require.NotContains(t, target, "/fc_x86_64",
			"symlink %s should not contain platform prefix in target: %s", name, target)
	}
}

func TestBuildMultiPlatformIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeDockerfile(t, dockerfile)
	opts := BuildOpts{
		Runtime: "unikraft.io/unikraft.org/base",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{
			{OS: "fc", Architecture: "x86_64"},
			{OS: "qemu", Architecture: "x86_64"},
		},
	}

	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 2)
	assertPlatforms(t, imgs, []string{"fc/x86_64", "qemu/x86_64"})
}

func TestBuildSecretsIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM busybox:latest
RUN --mount=type=secret,id=api_key cat /run/secrets/api_key | grep -q s3cr3t
`
	rootfsPath := writeDockerfile(t, dockerfile)
	secretPath := writeSecretFile(t, "s3cr3t\n")
	secrets, err := buildflags.ParseSecretSpecs([]string{"id=api_key,src=" + secretPath})
	require.NoError(t, err)

	opts := BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Secrets:  secrets,
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}
	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertPlatforms(t, imgs, []string{"kraftcloud/x86_64"})
}

func TestBuildCmdEnvLabelsIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM scratch
LABEL com.example.source=dockerfile com.example.only=dockerfile
ENV DOCKERFILE_ENV=from-dockerfile
CMD ["/dockerfile-cmd"]
`
	rootfsPath := writeDockerfile(t, dockerfile)
	opts := BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
		Cmd:      []string{"/unikraft-cmd", "--from-opts"},
		Env: kraftfile.Map{
			{Key: "OPTS_ENV", Value: "from-opts"},
			{Key: "OPTS_FLAG", Value: "1"},
		},
		Labels: map[string]string{
			"com.example.source": "opts",
			"com.example.extra":  "true",
		},
	}

	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	require.NotNil(t, imgs[0])
	require.NotNil(t, imgs[0].Image)
	require.Equal(t, opts.Cmd, imgs[0].Image.Config.Cmd)
	require.Equal(t, opts.Labels, imgs[0].Image.Config.Labels)
	require.Contains(t, imgs[0].Image.Config.Env, "OPTS_ENV=from-opts")
	require.Contains(t, imgs[0].Image.Config.Env, "OPTS_FLAG=1")
	require.Contains(t, imgs[0].Image.Config.Env, "DOCKERFILE_ENV=from-dockerfile")
}

func TestBuildDockerfileLabelsIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM scratch
LABEL com.example.source=dockerfile com.example.only=dockerfile
`
	imgs := runBuild(t, ctx, BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   writeDockerfile(t, dockerfile),
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)
	require.NotNil(t, imgs[0])
	require.NotNil(t, imgs[0].Image)
	require.Equal(t, "dockerfile", imgs[0].Image.Config.Labels["com.example.source"])
	require.Equal(t, "dockerfile", imgs[0].Image.Config.Labels["com.example.only"])
}

func integrationContext(t *testing.T) context.Context {
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

func writeDockerfile(t *testing.T, dockerfile string) string {
	t.Helper()
	dir := t.TempDir()
	dockerfilePath := filepath.Join(dir, "Dockerfile")
	require.NoError(t, os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644))
	return dockerfilePath
}

func writeSecretFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func runBuild(t *testing.T, ctx context.Context, opts BuildOpts) []*imagespec.Image {
	t.Helper()
	bkc, cleanup, err := buildkit.ConnectToBuildkit(ctx)
	if err != nil {
		t.Skipf("buildkit unavailable: %v", err)
	}
	if cleanup != nil {
		t.Cleanup(cleanup)
	}
	ctx = buildkit.WithBuildkitContext(ctx, bkc)

	imgs, err := Build(ctx, opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, img := range imgs {
			_ = img.Close()
		}
	})

	return imgs
}

func assertPlatforms(t *testing.T, imgs []*imagespec.Image, expected []string) {
	t.Helper()
	platformsFound := make([]string, 0, len(imgs))
	for _, img := range imgs {
		require.NotNil(t, img)
		require.NotNil(t, img.Image)
		platformsFound = append(platformsFound, platforms.Format(img.Image.Platform))
	}
	require.ElementsMatch(t, expected, platformsFound)
}

func TestBuildCharDeviceErofsIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM busybox:latest
RUN mknod /testfile c 220 220
`
	rootfsPath := writeDockerfile(t, dockerfile)
	opts := BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeErofs,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

	erofsImg := openErofsInitrdImage(t, imgs[0])
	inodes := walkErofsInodes(t, erofsImg)

	require.Contains(t, inodes, "testfile")
	ino := inodes["testfile"]
	require.True(t, ino.IsCharDev(), "testfile should be a character device node")
	require.Equal(t, uint32(220), ino.GetDevMajor(), "char device major number should be preserved")
	require.Equal(t, uint32(220), ino.GetDevMinor(), "char device minor number should be preserved")
}

func TestBuildHiddenFileErofsIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM scratch
COPY <<'EOF' /.hidden
hidden content
EOF
`
	rootfsPath := writeDockerfile(t, dockerfile)
	opts := BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeErofs,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

	files := readErofsInitrd(t, imgs[0])
	require.Contains(t, files, ".hidden", "hidden file must be preserved with its leading dot")
	require.Equal(t, "hidden content\n", files[".hidden"])
}

func TestBuildCustomDockerfileNameViaPathIntegration(t *testing.T) {
	ctx := integrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	dir := t.TempDir()
	dockerfilePath := filepath.Join(dir, "MyDockerfile")
	require.NoError(t, os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644))

	opts := BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: FSOpts{
			Format: kraftfile.FsTypeErofs,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   dockerfilePath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

	files := readErofsInitrd(t, imgs[0])
	require.Contains(t, files, "hello.txt")
	require.Equal(t, "hello\n", files["hello.txt"])
}

// openErofsInitrdImage opens the initrd from img as an erofs.Image.
func openErofsInitrdImage(t *testing.T, img *imagespec.Image) *goerofs.Image {
	t.Helper()
	require.NotNil(t, img.Initrd)

	ctx := t.Context()
	rc, _, err := img.Initrd.Open(ctx)
	require.NoError(t, err)
	defer rc.Close()

	tmp, err := os.CreateTemp(t.TempDir(), "erofs-*.img")
	require.NoError(t, err)
	t.Cleanup(func() { tmp.Close() })

	_, err = io.Copy(tmp, rc)
	require.NoError(t, err)

	erofsImg, err := goerofs.OpenImage(tmp)
	require.NoError(t, err)

	return erofsImg
}

// walkErofsInodes walks inodes in erofsImg and returns a path -> Inode map.
func walkErofsInodes(t *testing.T, erofsImg *goerofs.Image) map[string]goerofs.Inode {
	t.Helper()
	result := make(map[string]goerofs.Inode)

	var walk func(prefix string, nid uint64)
	walk = func(prefix string, nid uint64) {
		ino, err := erofsImg.Inode(nid)
		require.NoError(t, err)

		err = ino.IterDirents(func(name string, _ uint8, childNid uint64) error {
			if name == "." || name == ".." {
				return nil
			}

			path := name
			if prefix != "" {
				path = prefix + "/" + name
			}

			childIno, err := erofsImg.Inode(childNid)
			if err != nil {
				return err
			}
			result[path] = childIno
			if childIno.IsDir() {
				walk(path, childNid)
			}
			return nil
		})
		require.NoError(t, err)
	}

	walk("", erofsImg.RootNid())
	return result
}
