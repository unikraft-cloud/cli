// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/platforms"
	"github.com/moby/buildkit/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	archivefs "github.com/unikraft/go-archivefs"
	goerofs "github.com/unikraft/go-archivefs/erofs"
	"github.com/unikraft/go-cpio"
	"unikraft.com/x/build"
	"unikraft.com/x/build/buildflags"
	"unikraft.com/x/build/progress"
	"unikraft.com/x/buildkit"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/integration"
)

func TestBuildSinglePlatformIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	opts := build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runIntegrationBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertBuildPlatforms(t, imgs, []string{"kraftcloud/x86_64"})
}

func TestBuildSinglePlatformErofsIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	opts := build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeErofs,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runIntegrationBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertBuildPlatforms(t, imgs, []string{"kraftcloud/x86_64"})
}

func TestBuildDefaultErofsFormatBaseCompatIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	opts := build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
			Type: kraftfile.SourceTypeDockerfile,
			Path: rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runIntegrationBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertBuildPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

	files := readErofsInitrd(t, imgs[0])
	require.Contains(t, files, "hello.txt")
	require.Equal(t, "hello\n", files["hello.txt"])
}

func TestBuildAbsoluteSymlinksIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
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
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	opts := build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runIntegrationBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertBuildPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

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
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	opts := build.BuildOpts{
		Runtime: "unikraft.io/unikraft.org/base",
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{
			{OS: "fc", Architecture: "x86_64"},
			{OS: "qemu", Architecture: "x86_64"},
		},
	}

	imgs := runIntegrationBuild(t, ctx, opts)
	require.Len(t, imgs, 2)
	assertBuildPlatforms(t, imgs, []string{"fc/x86_64", "qemu/x86_64"})
}

func TestBuildSecretsIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM busybox:latest
RUN --mount=type=secret,id=api_key cat /run/secrets/api_key | grep -q s3cr3t
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	secretPath := writeBuildSecret(t, "s3cr3t\n")
	secrets, err := buildflags.ParseSecretSpecs([]string{"id=api_key,src=" + secretPath})
	require.NoError(t, err)

	opts := build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Secrets:  secrets,
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}
	imgs := runIntegrationBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertBuildPlatforms(t, imgs, []string{"kraftcloud/x86_64"})
}

func TestBuildCmdEnvLabelsIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch
LABEL com.example.source=dockerfile com.example.only=dockerfile
ENV DOCKERFILE_ENV=from-dockerfile
CMD ["/dockerfile-cmd"]
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	opts := build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
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

	imgs := runIntegrationBuild(t, ctx, opts)
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
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch
LABEL com.example.source=dockerfile com.example.only=dockerfile
`
	imgs := runIntegrationBuild(t, ctx, build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeCpio,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   writeBuildDockerfile(t, dockerfile),
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)
	require.NotNil(t, imgs[0])
	require.NotNil(t, imgs[0].Image)
	require.Equal(t, "dockerfile", imgs[0].Image.Config.Labels["com.example.source"])
	require.Equal(t, "dockerfile", imgs[0].Image.Config.Labels["com.example.only"])
}

func buildIntegrationContext(t *testing.T) context.Context {
	t.Helper()
	integration.SkipUnlessIntegration(t)
	integration.RequireBuildkit(t)
	t.Setenv("BUILDKIT_PROGRESS", "quiet")

	ctx := t.Context()
	logger, err := log.New(ctx, log.Config{Sink: t.Output(), Type: log.TextType, Level: log.InfoLevel})
	require.NoError(t, err)
	ctx = log.WithLogger(ctx, logger)
	cfg, err := integration.LoadRemote(t)
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

func writeBuildDockerfile(t *testing.T, dockerfile string) string {
	t.Helper()
	dir := t.TempDir()
	dockerfilePath := filepath.Join(dir, "Dockerfile")
	require.NoError(t, os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644))
	return dockerfilePath
}

func writeBuildSecret(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func runIntegrationBuild(t *testing.T, ctx context.Context, opts build.BuildOpts) []*imagespec.Image {
	t.Helper()
	imgs, err := integrationBuilder(t, ctx).Build(ctx, opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, img := range imgs {
			_ = img.Close()
		}
	})

	return imgs
}

// runIntegrationRootfs calls BuildRootfs with the builder of the CLI.
func runIntegrationRootfs(t *testing.T, ctx context.Context, opts build.BuildOpts) []*imagespec.Image {
	t.Helper()
	imgs, err := integrationBuilder(t, ctx).BuildRootfs(ctx, opts)
	require.NoError(t, err)
	t.Cleanup(func() {
		for _, img := range imgs {
			_ = img.Close()
		}
	})
	return imgs
}

// integrationBuilder gives the builder of the CLI on one BuildKit connection,
// and skips the test when no daemon answers.
func integrationBuilder(t *testing.T, ctx context.Context) build.Builder {
	t.Helper()
	d, err := localBuildkit.Acquire(ctx, buildkit.Key{})
	if err != nil {
		t.Skipf("buildkit unavailable: %v", err)
	}
	t.Cleanup(func() { _ = d.Release() })

	b := newBuilder(progress.Discard)
	b.Buildkit = func(context.Context) (*client.Client, func(), error) {
		return d.Client, nil, nil
	}
	return b
}

func assertBuildPlatforms(t *testing.T, imgs []*imagespec.Image, expected []string) {
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
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM busybox:latest
RUN mknod /testfile c 220 220
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	opts := build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeErofs,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runIntegrationBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertBuildPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

	erofsImg := openErofsInitrdImage(t, imgs[0])
	inodes := walkErofsInodes(t, erofsImg)

	require.Contains(t, inodes, "testfile")
	ino := inodes["testfile"]
	require.True(t, ino.IsCharDev(), "testfile should be a character device node")
	require.Equal(t, uint32(220), ino.GetDevMajor(), "char device major number should be preserved")
	require.Equal(t, uint32(220), ino.GetDevMinor(), "char device minor number should be preserved")
}

func TestBuildHiddenFileErofsIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch
COPY <<'EOF' /.hidden
hidden content
EOF
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	opts := build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeErofs,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   rootfsPath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runIntegrationBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertBuildPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

	files := readErofsInitrd(t, imgs[0])
	require.Contains(t, files, ".hidden", "hidden file must be preserved with its leading dot")
	require.Equal(t, "hidden content\n", files[".hidden"])
}

func TestBuildCustomDockerfileNameViaPathIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	dir := t.TempDir()
	dockerfilePath := filepath.Join(dir, "MyDockerfile")
	require.NoError(t, os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644))

	opts := build.BuildOpts{
		Runtime: "unikraft.io/official/base-compat",
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeErofs,
			Type:   kraftfile.SourceTypeDockerfile,
			Path:   dockerfilePath,
		},
		Platform: []ocispec.Platform{{OS: "kraftcloud", Architecture: "x86_64"}},
	}

	imgs := runIntegrationBuild(t, ctx, opts)
	require.Len(t, imgs, 1)
	assertBuildPlatforms(t, imgs, []string{"kraftcloud/x86_64"})

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

func TestRootfsDockerfileCpioIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
		Rootfs: build.FSOpts{
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

func TestRootfsDockerfileLabelsIntegration(t *testing.T) {
	dockerfile := `
FROM scratch
LABEL com.example.source=dockerfile com.example.only=dockerfile
`

	t.Run("preserved when no labels are given", func(t *testing.T) {
		ctx := buildIntegrationContext(t)
		imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
			Rootfs: build.FSOpts{
				Format: kraftfile.FsTypeCpio,
				Path:   writeBuildDockerfile(t, dockerfile),
				Type:   kraftfile.SourceTypeDockerfile,
			},
			Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
		})
		require.Len(t, imgs, 1)
		require.Equal(t, "dockerfile", imgs[0].Image.Config.Labels["com.example.source"])
		require.Equal(t, "dockerfile", imgs[0].Image.Config.Labels["com.example.only"])
	})

	t.Run("replaced by the given labels", func(t *testing.T) {
		ctx := buildIntegrationContext(t)
		imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
			Rootfs: build.FSOpts{
				Format: kraftfile.FsTypeCpio,
				Path:   writeBuildDockerfile(t, dockerfile),
				Type:   kraftfile.SourceTypeDockerfile,
			},
			Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
			Labels:   map[string]string{"com.example.source": "opts"},
		})
		require.Len(t, imgs, 1)
		require.Equal(t, map[string]string{"com.example.source": "opts"}, imgs[0].Image.Config.Labels)
	})
}

func TestRootfsDockerfileErofsIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
		Rootfs: build.FSOpts{
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
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY <<'EOF' /hello.txt
hello
EOF
`
	contextDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "MyDockerfile"), []byte(dockerfile), 0o644))

	imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
		Rootfs: build.FSOpts{
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
	ctx := buildIntegrationContext(t)
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
	rootfsPath := writeBuildDockerfile(t, dockerfile)

	for _, format := range []kraftfile.FsType{kraftfile.FsTypeCpio, kraftfile.FsTypeErofs} {
		t.Run(string(format), func(t *testing.T) {
			imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
				Rootfs: build.FSOpts{
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

func TestRootfsDockerfileKeepsOwnersIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	dockerfile := `
FROM scratch

COPY --chown=1000:1000 <<'EOF' /owned.txt
owned
EOF
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
		Rootfs: build.FSOpts{
			Format: kraftfile.FsTypeErofs,
			Path:   rootfsPath,
			Type:   kraftfile.SourceTypeDockerfile,
		},
		Platform: []ocispec.Platform{{OS: "fc", Architecture: "x86_64"}},
	})
	require.Len(t, imgs, 1)

	owners := readOwners(t, imgs[0].Initrd, kraftfile.FsTypeErofs)
	require.Equal(t, owner{1000, 1000}, owners["owned.txt"])
}

func TestRootfsMultiPlatform(t *testing.T) {
	ctx := buildIntegrationContext(t)
	// $BUILDPLATFORM avoids emulation; TARGETARCH still varies per platform.
	dockerfile := `
FROM --platform=$BUILDPLATFORM busybox AS base
ARG TARGETARCH
RUN echo "$TARGETARCH" > /arch.txt

FROM scratch
COPY --from=base /arch.txt /arch.txt
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
		Rootfs: build.FSOpts{
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

	ctx := buildIntegrationContext(t)
	// Unlike above, RUN executes under the target platform here, requiring
	// QEMU emulation rather than just varying TARGETARCH.
	dockerfile := `
FROM busybox AS base
RUN uname -m > /arch.txt

FROM scratch
COPY --from=base /arch.txt /arch.txt
`
	rootfsPath := writeBuildDockerfile(t, dockerfile)
	imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
		Rootfs: build.FSOpts{
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

// TestRootfsOCIRegularImageIntegration reads a regular OCI image (plain layers,
// no unikraft components) from a registry and verifies that BuildKit flattens
// the layers and that the result is re-packaged into the requested rootfs
// format.
func TestRootfsOCIRegularImageIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	ref := integration.HelloWorld.Mirror(t, ctx)

	for _, format := range []kraftfile.FsType{kraftfile.FsTypeCpio, kraftfile.FsTypeErofs} {
		t.Run(string(format), func(t *testing.T) {
			imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
				Rootfs: build.FSOpts{
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
	ctx := buildIntegrationContext(t)
	imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
		Rootfs: build.FSOpts{
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
	ctx := buildIntegrationContext(t)
	imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
		Rootfs: build.FSOpts{
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
	assertBuildPlatforms(t, imgs, []string{"fc/x86_64", "fc/arm64"})
}

// TestRootfsOCIRegularImageSharedFlattenIntegration covers two unikraft
// platforms that normalise onto the same linux platform: they share one source
// image, so the flatten is done once and reused rather than solved per platform.
func TestRootfsOCIRegularImageSharedFlattenIntegration(t *testing.T) {
	ctx := buildIntegrationContext(t)
	imgs := runIntegrationRootfs(t, ctx, build.BuildOpts{
		Rootfs: build.FSOpts{
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
	assertBuildPlatforms(t, imgs, []string{"fc/x86_64", "qemu/x86_64"})
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

// owner is the uid and gid of an entry in a built root filesystem.
type owner struct{ uid, gid int }

// readOwners returns the owner of every entry in the filesystem image f.
func readOwners(t *testing.T, f imagespec.File, format kraftfile.FsType) map[string]owner {
	t.Helper()
	require.NotNil(t, f)

	rc, _, err := f.Open(t.Context())
	require.NoError(t, err)
	defer rc.Close()

	owners := make(map[string]owner)
	switch format {
	case kraftfile.FsTypeCpio:
		cr := cpio.NewReader(rc)
		for {
			hdr, err := cr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, cr)
			require.NoError(t, err)
			owners[strings.TrimPrefix(hdr.Name, "./")] = owner{hdr.Uid, hdr.Guid}
		}
	case kraftfile.FsTypeErofs:
		// EROFS needs io.ReaderAt; copy the stream into a temporary file.
		tmp, err := os.CreateTemp(t.TempDir(), "erofs-*.img")
		require.NoError(t, err)
		defer tmp.Close()
		_, err = io.Copy(tmp, rc)
		require.NoError(t, err)

		fsys, err := goerofs.Open(tmp)
		require.NoError(t, err)
		require.NoError(t, fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || path == "." {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			owners[path] = owner{archivefs.GetUID(info.Sys()), archivefs.GetGID(info.Sys())}
			return nil
		}))
	default:
		t.Fatalf("unknown format %q", format)
	}
	return owners
}

func TestResolveNamedBuildContext(t *testing.T) {
	t.Run("local directory", func(t *testing.T) {
		dir := t.TempDir()

		got, err := resolveNamedBuildContext("shared", dir)
		require.NoError(t, err)
		require.IsType(t, build.LocalSource{}, got)

		local := got.(build.LocalSource)

		want, err := filepath.Abs(dir)
		require.NoError(t, err)
		require.Equal(t, want, local.Dir)
	})

	t.Run("docker image reference", func(t *testing.T) {
		const ref = "docker-image://alpine:latest"

		got, err := resolveNamedBuildContext("base", ref)
		require.NoError(t, err)
		require.IsType(t, build.FrontendSource{}, got)

		frontend := got.(build.FrontendSource)
		require.Equal(t, ref, frontend.Ref)
	})

	t.Run("OCI layout reference", func(t *testing.T) {
		const ref = "oci-layout:///tmp/layout"

		got, err := resolveNamedBuildContext("base", ref)
		require.NoError(t, err)
		require.IsType(t, build.FrontendSource{}, got)

		frontend := got.(build.FrontendSource)
		require.Equal(t, ref, frontend.Ref)
	})

	t.Run("missing directory", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")

		_, err := resolveNamedBuildContext("missing", missing)
		require.Error(t, err)
	})

	t.Run("file instead of directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "context.txt")
		require.NoError(t, os.WriteFile(file, []byte("test"), 0o600))

		_, err := resolveNamedBuildContext("shared", file)
		require.Error(t, err)
	})

	t.Run("HTTPS Git reference", func(t *testing.T) {
		const ref = "https://github.com/unikraft-cloud/examples.git"

		got, err := resolveNamedBuildContext("shared", ref)
		require.NoError(t, err)
		require.IsType(t, build.FrontendSource{}, got)

		frontend := got.(build.FrontendSource)
		require.Equal(t, ref, frontend.Ref)
	})

	t.Run("HTTP reference", func(t *testing.T) {
		const ref = "http://example.com/context.tar.gz"

		got, err := resolveNamedBuildContext("shared", ref)
		require.NoError(t, err)
		require.IsType(t, build.FrontendSource{}, got)

		frontend := got.(build.FrontendSource)
		require.Equal(t, ref, frontend.Ref)
	})

	t.Run("Git protocol reference", func(t *testing.T) {
		const ref = "git://github.com/unikraft-cloud/examples.git"

		got, err := resolveNamedBuildContext("shared", ref)
		require.NoError(t, err)
		require.IsType(t, build.FrontendSource{}, got)

		frontend := got.(build.FrontendSource)
		require.Equal(t, ref, frontend.Ref)
	})

	t.Run("SSH reference", func(t *testing.T) {
		const ref = "ssh://git@example.com/repo.git"

		got, err := resolveNamedBuildContext("shared", ref)
		require.NoError(t, err)
		require.IsType(t, build.FrontendSource{}, got)

		frontend := got.(build.FrontendSource)
		require.Equal(t, ref, frontend.Ref)
	})

	t.Run("unsupported URL scheme", func(t *testing.T) {
		_, err := resolveNamedBuildContext(
			"remote",
			"ftp://example.com/context.tar.gz",
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "unsupported source scheme")
	})

	t.Run("empty source", func(t *testing.T) {
		_, err := resolveNamedBuildContext("shared", "")
		require.Error(t, err)
		require.ErrorContains(t, err, "empty source")
	})

	t.Run("empty image reference", func(t *testing.T) {
		_, err := resolveNamedBuildContext("base", "docker-image://")
		require.Error(t, err)
		require.ErrorContains(t, err, "empty image reference")
	})

	t.Run("empty OCI layout reference", func(t *testing.T) {
		_, err := resolveNamedBuildContext("base", "oci-layout://")
		require.Error(t, err)
		require.ErrorContains(t, err, "empty OCI layout reference")
	})
}
