// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/containerd/platforms"
	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/progress/progresswriter"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	imagespec "unikraft.com/x/image-spec"

	goerofs "github.com/unikraft/go-archivefs/erofs"
	gotar "github.com/unikraft/go-archivefs/tarfs"
	gocpio "github.com/unikraft/go-cpio"
	"unikraft.com/cli/internal/builder/buildflags"
	"unikraft.com/cli/internal/builder/buildfs"
	"unikraft.com/cli/internal/buildkit"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"
)

// buildImageConfig constructs a minimal OCI image config from build options.
// Used when a BuildKit solve is not performed.
func buildImageConfig(opts BuildOpts) ocispec.ImageConfig {
	var cfg ocispec.ImageConfig
	if opts.Cmd != nil {
		cfg.Cmd = opts.Cmd
	}
	if opts.Env != nil {
		env := make([]string, 0, len(opts.Env))
		for _, kv := range opts.Env {
			env = append(env, fmt.Sprintf("%s=%s", kv.Key, kv.Value))
		}
		cfg.Env = env
	}
	cfg.Labels = opts.Labels
	return cfg
}

// DetectSourceType inspects path and returns the detected RootfsType.
func DetectSourceType(path string) (kraftfile.SourceType, error) {
	if path == "" {
		return "", fmt.Errorf("empty rootfs path")
	}

	base := filepath.Base(path)
	if base == "Dockerfile" || slices.Contains(strings.Split(base, "."), "Dockerfile") {
		return kraftfile.SourceTypeDockerfile, nil
	}

	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("rootfs path does not exist")
		}
		return "", fmt.Errorf("checking rootfs source %q: %w", path, err)
	}

	switch {
	case fi.IsDir():
		return kraftfile.SourceTypeDirectory, nil
	case fi.Mode().IsRegular(), fi.Mode()&os.ModeSymlink != 0:
		if gocpio.IsValidPath(path) {
			return kraftfile.SourceTypeCpio, nil
		}
		if goerofs.IsValidPath(path) {
			return kraftfile.SourceTypeErofs, nil
		}
		if f, err := os.Open(path); err == nil {
			defer f.Close()
			r, err := buildfs.MaybeGunzip(f)
			if err != nil {
				return "", nil
			}
			if gotar.IsValid(r) {
				return kraftfile.SourceTypeTarball, nil
			}
		}
		return "", fmt.Errorf("could not detect file rootfs type %q", path)
	default:
		return "", fmt.Errorf("could not detect rootfs type %q", path)
	}
}

func BuildRoms(ctx context.Context, opts BuildOpts) (_ [][]imagespec.File, rerr error) {
	var romFiles [][]imagespec.File

	// Release anything already built if a later ROM fails.
	defer func() {
		if rerr == nil {
			return
		}
		for _, perPlat := range romFiles {
			for _, f := range perPlat {
				if f != nil {
					_ = f.Cleanup()
				}
			}
		}
	}()

	for _, rom := range opts.Roms {
		romBuildOpts := BuildOpts{
			Rootfs: FSOpts{
				Path:   rom.Path,
				Type:   rom.Type,
				Format: cmp.Or(rom.Format, kraftfile.FsTypeErofs),
				Pad:    rom.Pad,
			},
			// propagate BuildKit options from the parent build
			Platform: opts.Platform,
			BuildArg: opts.BuildArg,
			Target:   opts.Target,
			Secrets:  opts.Secrets,
			SSH:      opts.SSH,
			NoCache:  opts.NoCache,
		}

		imgs, err := BuildRootfs(ctx, romBuildOpts)
		if err != nil {
			return nil, fmt.Errorf("building rom from %q: %w", rom.Path, err)
		}
		if len(imgs) != len(opts.Platform) {
			return nil, fmt.Errorf("rom build from %q produced %d images for %d platforms",
				rom.Path, len(imgs), len(opts.Platform))
		}

		perPlat := make([]imagespec.File, len(imgs))
		for i, img := range imgs {
			if img.Initrd == nil {
				return nil, fmt.Errorf("rom build from %q produced no output for platform %s",
					rom.Path, platforms.Format(opts.Platform[i]))
			}
			perPlat[i] = img.Initrd
			img.Initrd = nil // detach so Close doesn't close it
			img.Close()
		}

		romFiles = append(romFiles, perPlat)
	}
	return romFiles, nil
}

// BuildRootfs builds a rootfs for each platform in opts.Platform from the
// source at opts.Rootfs.Path.
func BuildRootfs(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	if len(opts.Platform) == 0 {
		return nil, fmt.Errorf("at least one platform must be specified")
	}

	if opts.Rootfs.Format == "" &&
		opts.Rootfs.Type != kraftfile.SourceTypeCpio &&
		opts.Rootfs.Type != kraftfile.SourceTypeErofs {
		opts.Rootfs.Format = DefaultRootfsFormat(opts.Platform)
	}

	switch opts.Rootfs.Type {
	case kraftfile.SourceTypeCpio, kraftfile.SourceTypeErofs:
		expected := map[kraftfile.SourceType]kraftfile.FsType{
			kraftfile.SourceTypeCpio:  kraftfile.FsTypeCpio,
			kraftfile.SourceTypeErofs: kraftfile.FsTypeErofs,
		}
		if opts.Rootfs.Format != "" {
			if exp, ok := expected[opts.Rootfs.Type]; ok && opts.Rootfs.Format != exp {
				// TODO: maybe we could be smart and convert this
				return nil, fmt.Errorf("unsupported rootfs format mismatch: source is %s but requested format is %s", opts.Rootfs.Type, opts.Rootfs.Format)
			}
		} else {
			opts.Rootfs.Format = expected[opts.Rootfs.Type]
		}
		if opts.Rootfs.Type == kraftfile.SourceTypeCpio && !gocpio.IsValidPath(opts.Rootfs.Path) ||
			opts.Rootfs.Type == kraftfile.SourceTypeErofs && !goerofs.IsValidPath(opts.Rootfs.Path) {
			return nil, fmt.Errorf("malformed rootfs %s file %q", opts.Rootfs.Type, opts.Rootfs.Path)
		}

		return buildRootfsPackaged(ctx, opts)
	case kraftfile.SourceTypeDirectory:
		return buildRootfsDirectory(ctx, opts)
	case kraftfile.SourceTypeTarball:
		return buildRootfsTarball(ctx, opts)
	case kraftfile.SourceTypeDockerfile:
		if _, err := os.Stat(opts.Rootfs.Path); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("dockerfile context does not exist")
			}
			return nil, fmt.Errorf("checking dockerfile context path %q: %w", opts.Rootfs.Path, err)
		}
		if opts.Rootfs.Dockerfile != "" {
			dockerfilePath := filepath.Join(opts.Rootfs.Path, opts.Rootfs.Dockerfile)
			if _, err := os.Stat(dockerfilePath); err != nil {
				if os.IsNotExist(err) {
					return nil, fmt.Errorf("dockerfile %q does not exist in context %q", opts.Rootfs.Dockerfile, opts.Rootfs.Path)
				}
				return nil, fmt.Errorf("checking dockerfile path %q: %w", dockerfilePath, err)
			}
		}
		return buildRootfsDockerfile(ctx, opts)
	default:
		return nil, fmt.Errorf("unsupported rootfs type %q", opts.Rootfs.Type)
	}
}

// buildRootfsFromPackaged returns images backed by an already-packaged rootfs
// file. The file is opened read-only per platform.
// The caller must not delete it.
func buildRootfsPackaged(_ context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	cfg := buildImageConfig(opts)

	var imgs []*imagespec.Image
	for _, p := range opts.Platform {
		f, err := os.Open(opts.Rootfs.Path)
		if err != nil {
			return nil, fmt.Errorf("opening pre-packaged rootfs: %w", err)
		}
		defer func() {
			if rerr != nil {
				f.Close()
			}
		}()

		imgs = append(imgs, imagespec.NewImage(
			imagespec.WithImageConfig(cfg),
			imagespec.WithPlatform(p),
			imagespec.WithInitrd(imagespec.NewOSFile(f)),
		))
	}
	return imgs, nil
}

// buildRootfsFromDirectory archives the source directory into a temporary
// rootfs file (CPIO or EroFS, based on opts.Rootfs.Format) for each platform.
func buildRootfsDirectory(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	cfg := buildImageConfig(opts)

	var imgs []*imagespec.Image
	for _, p := range opts.Platform {
		f, err := os.CreateTemp("", "unikraft-rootfs-*."+string(opts.Rootfs.Format))
		if err != nil {
			return nil, fmt.Errorf("could not create temporary file: %w", err)
		}
		defer func() {
			if rerr != nil && f != nil {
				f.Close()
				os.Remove(f.Name())
			}
		}()

		if err := packageFS(ctx, opts.Rootfs.Format, f, os.DirFS(opts.Rootfs.Path), opts.Rootfs); err != nil {
			return nil, err
		}

		imgs = append(imgs, imagespec.NewImage(
			imagespec.WithImageConfig(cfg),
			imagespec.WithPlatform(p),
			imagespec.WithInitrd(imagespec.NewTempOSFile(f)),
		))
	}
	return imgs, nil
}

// buildRootfsTarball opens the source tarball as an fs.FS and packages it
// into the requested rootfs format (CPIO or EroFS) for each platform.
func buildRootfsTarball(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	cfg := buildImageConfig(opts)

	tarFile, err := os.Open(opts.Rootfs.Path)
	if err != nil {
		return nil, fmt.Errorf("could not open tarball: %w", err)
	}
	defer tarFile.Close()

	srcFS, err := buildfs.TarballFS(tarFile)
	if err != nil {
		return nil, fmt.Errorf("could not open tarball as filesystem: %w", err)
	}

	var imgs []*imagespec.Image
	for _, p := range opts.Platform {
		f, err := os.CreateTemp("", "unikraft-rootfs-*."+string(opts.Rootfs.Format))
		if err != nil {
			return nil, fmt.Errorf("could not create temporary file: %w", err)
		}
		defer func() {
			if rerr != nil && f != nil {
				f.Close()
				os.Remove(f.Name())
			}
		}()

		if err := packageFS(ctx, opts.Rootfs.Format, f, srcFS, opts.Rootfs); err != nil {
			return nil, err
		}

		imgs = append(imgs, imagespec.NewImage(
			imagespec.WithImageConfig(cfg),
			imagespec.WithPlatform(p),
			imagespec.WithInitrd(imagespec.NewTempOSFile(f)),
		))
	}
	return imgs, nil
}

func buildRootfsDockerfile(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	dockerConfig := dockerconfig.LoadDefaultConfigFile(os.Stderr)

	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}

	session := []session.Attachable{
		authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
			AuthConfigProvider: images.LoadBuildkitAuthConfig(dockerConfig, profile),
		}),
	}

	attrs := map[string]string{}
	localDirs := map[string]string{}
	if err := applyBuildOpts(attrs, localDirs, &session, opts); err != nil {
		return nil, err
	}

	c, cleanup, err := buildkit.ConnectToBuildkit(ctx)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	expPlatforms := getPlatforms(opts.Platform)

	pw, err := progresswriter.NewPrinter(context.WithoutCancel(ctx), os.Stderr, "auto")
	if err != nil {
		return nil, err
	}
	mw := progresswriter.NewMultiWriter(pw)

	// Create upfront to avoid deadlock hazard.
	platformWriters := make([]progresswriter.Writer, len(expPlatforms))
	for i, ep := range expPlatforms {
		platformWriters[i] = mw.WithPrefix(ep.ID, true)
	}

	// NOTE: solving all platforms in one export corrupts symlinks (a
	// buildkit bug), so solve and export each platform separately:
	// https://github.com/moby/buildkit/issues/6684
	var imgs []*imagespec.Image
	for i, p := range opts.Platform {
		ep := expPlatforms[i]

		platformAttrs := maps.Clone(attrs)
		platformAttrs["platform"] = ep.ID

		tarDest, err := os.CreateTemp("", "unikraft-buildkit-*.tar")
		if err != nil {
			return nil, fmt.Errorf("could not create temporary file: %w", err)
		}
		tarDestPath := tarDest.Name()
		defer func() {
			tarDest.Close()
			os.Remove(tarDestPath)
		}()

		solveOpt := client.SolveOpt{
			Ref:     identity.NewID(),
			Session: session,
			Exports: []client.ExportEntry{
				{
					Type: client.ExporterTar,
					Output: func(map[string]string) (io.WriteCloser, error) {
						return tarDest, nil
					},
				},
			},
			LocalDirs:     localDirs,
			Frontend:      "dockerfile.v0",
			FrontendAttrs: platformAttrs,
		}

		platformWriter := platformWriters[i]

		var config ocispec.Image
		_, err = c.Build(ctx, solveOpt, "buildctl", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			res, err := c.Solve(ctx, gateway.SolveRequest{
				Frontend:    solveOpt.Frontend,
				FrontendOpt: solveOpt.FrontendAttrs,
			})
			if err != nil {
				return nil, err
			}
			cfg := exptypes.ParseKey(res.Metadata, "containerimage.config", &ep)
			if cfg == nil {
				return nil, fmt.Errorf("could not find config for platform %s in build result metadata", ep.ID)
			}
			if err := json.Unmarshal(cfg, &config); err != nil {
				return nil, err
			}
			return res, nil
		}, platformWriter.Status())
		if err != nil {
			return nil, err
		}

		// Reopen the tarball for reading.
		tarFile, err := os.Open(tarDestPath)
		if err != nil {
			return nil, fmt.Errorf("could not reopen tarball: %w", err)
		}
		defer tarFile.Close()

		srcFS, err := buildfs.TarballFS(tarFile)
		if err != nil {
			return nil, fmt.Errorf("could not open tarball as filesystem: %w", err)
		}

		f, err := os.CreateTemp("", "unikraft-rootfs-*."+string(opts.Rootfs.Format))
		if err != nil {
			return nil, fmt.Errorf("could not create temporary file: %w", err)
		}
		defer func() {
			if rerr != nil && f != nil {
				f.Close()
				os.Remove(f.Name())
			}
		}()

		if err := packageFS(ctx, opts.Rootfs.Format, f, srcFS, opts.Rootfs); err != nil {
			return nil, err
		}

		if opts.Cmd != nil {
			config.Config.Cmd = opts.Cmd
		}
		if opts.Env != nil {
			env := make([]string, 0, len(opts.Env))
			for _, kv := range opts.Env {
				env = append(env, fmt.Sprintf("%s=%s", kv.Key, kv.Value))
			}
			config.Config.Env = append(env, config.Config.Env...)
		}
		config.Config.Labels = opts.Labels

		imgs = append(imgs, imagespec.NewImage(
			imagespec.WithImageConfig(config.Config),
			imagespec.WithPlatform(p),
			imagespec.WithInitrd(imagespec.NewTempOSFile(f)),
		))
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-pw.Done():
	}
	if pw.Err() != nil {
		return nil, pw.Err()
	}

	return imgs, nil
}

func packageFS(ctx context.Context, format kraftfile.FsType, destFS *os.File, srcFS fs.FS, opts FSOpts) error {
	log.G(ctx).
		Debug().
		Str("format", string(format)).
		Msg("packaging rootfs")

	switch format {
	case kraftfile.FsTypeCpio:
		var gw *gzip.Writer
		var w io.Writer = destFS
		if opts.Compress {
			gw = gzip.NewWriter(w)
			w = gw
		}

		if err := buildfs.CreateCPIO(ctx, w, srcFS,
			buildfs.WithAllRoot(!opts.KeepOwners),
		); err != nil {
			return fmt.Errorf("could not create CPIO archive: %w", err)
		}

		if gw != nil {
			if err := gw.Close(); err != nil {
				return fmt.Errorf("could not close gzip writer: %w", err)
			}
		}
	case kraftfile.FsTypeErofs:
		if opts.Compress {
			log.G(ctx).Warn().Msg("compression is not supported for EROFS, ignoring compress option")
		}

		if err := buildfs.CreateEROFS(destFS, srcFS,
			buildfs.WithAllRoot(!opts.KeepOwners),
		); err != nil {
			return fmt.Errorf("could not create EroFS archive: %w", err)
		}
	default:
		return fmt.Errorf("unknown filesystem type %q", format)
	}

	if opts.Pad > 0 {
		pos, err := destFS.Seek(0, io.SeekEnd)
		if err != nil {
			return fmt.Errorf("could not seek to end of file: %w", err)
		}
		if rem := pos % opts.Pad; rem != 0 {
			pad := make([]byte, opts.Pad-rem)
			if _, err := destFS.Write(pad); err != nil {
				return fmt.Errorf("could not pad file to page alignment: %w", err)
			}
		}
	}

	if err := destFS.Sync(); err != nil {
		return fmt.Errorf("could not sync file: %w", err)
	}

	return nil
}

func applyBuildOpts(attrs map[string]string, localDirs map[string]string, sessions *[]session.Attachable, opts BuildOpts) error {
	if opts.Rootfs.Dockerfile != "" {
		localDirs["context"] = opts.Rootfs.Path
		localDirs["dockerfile"] = filepath.Join(opts.Rootfs.Path, filepath.Dir(opts.Rootfs.Dockerfile))
		attrs["filename"] = filepath.Base(opts.Rootfs.Dockerfile)
	} else {
		localDirs["context"] = filepath.Dir(opts.Rootfs.Path)
		localDirs["dockerfile"] = filepath.Dir(opts.Rootfs.Path)
		attrs["filename"] = filepath.Base(opts.Rootfs.Path)
	}
	if opts.Target != "" {
		attrs["target"] = opts.Target
	}

	if opts.NoCache {
		attrs["no-cache"] = ""
	}

	for name, path := range opts.BuildContexts {
		localDirs[name] = path
		attrs["context:"+name] = "local:" + name
	}

	for _, buildArg := range opts.BuildArg {
		if buildArg == "" {
			continue
		}
		key, val, ok := strings.Cut(buildArg, "=")
		if key == "" {
			return fmt.Errorf("invalid build-arg %q", buildArg)
		}
		if !ok {
			val, _ = os.LookupEnv(key)
		}
		attrs["build-arg:"+key] = val
	}

	if len(opts.Secrets) > 0 {
		provider, err := buildflags.CreateSecrets(opts.Secrets)
		if err != nil {
			return err
		}
		*sessions = append(*sessions, provider)
	}
	if len(opts.SSH) > 0 {
		provider, err := buildflags.CreateSSH(opts.SSH)
		if err != nil {
			return err
		}
		*sessions = append(*sessions, provider)
	}

	return nil
}

func getPlatforms(ps []ocispec.Platform) (exp []exptypes.Platform) {
	for _, platform := range ps {
		platform.OS = "linux"
		platform.OSFeatures = nil
		platform.OSVersion = ""
		platform = platforms.Normalize(platform)
		exp = append(exp, exptypes.Platform{
			ID:       platforms.Format(platform),
			Platform: platform,
		})
	}
	return exp
}
