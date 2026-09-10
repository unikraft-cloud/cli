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
	"github.com/moby/buildkit/client/llb"
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
	ukio "unikraft.com/x/io"
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"
)

// applyConfigOverrides layers the build options' Cmd and Env on top of base,
// which is the config of the image the rootfs was built from. Env is prepended
// so the caller's values take precedence. Labels replace the base's entirely.
func applyConfigOverrides(base ocispec.ImageConfig, opts BuildOpts) ocispec.ImageConfig {
	cfg := base
	if opts.Cmd != nil {
		cfg.Cmd = opts.Cmd
	}
	if opts.Env != nil {
		env := make([]string, 0, len(opts.Env)+len(cfg.Env))
		for _, kv := range opts.Env {
			env = append(env, fmt.Sprintf("%s=%s", kv.Key, kv.Value))
		}
		cfg.Env = append(env, cfg.Env...)
	}
	cfg.Labels = opts.Labels
	return cfg
}

// buildImageConfig constructs a minimal OCI image config from build options.
// Used when there is no source image config to build on top of.
func buildImageConfig(opts BuildOpts) ocispec.ImageConfig {
	return applyConfigOverrides(ocispec.ImageConfig{}, opts)
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
		return "", fmt.Errorf("checking rootfs source: %w", err)
	}

	switch {
	case fi.IsDir():
		return kraftfile.SourceTypeDirectory, nil
	case fi.Mode().IsRegular(), fi.Mode()&os.ModeSymlink != 0:
		if format, err := detectPackagedFormat(path); err == nil {
			return kraftfile.SourceType(format), nil
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

// detectPackagedFormat reports the rootfs format of an already-packaged file.
func detectPackagedFormat(path string) (kraftfile.FsType, error) {
	switch {
	case gocpio.IsValidPath(path):
		return kraftfile.FsTypeCpio, nil
	case goerofs.IsValidPath(path):
		return kraftfile.FsTypeErofs, nil
	default:
		return "", fmt.Errorf("could not detect rootfs format of %q", path)
	}
}

// resolveSource resolves the source of fsOpts against root and fills in the
// source type when it was not requested explicitly.
func resolveSource(root string, fsOpts *FSOpts) error {
	if fsOpts.Type == kraftfile.SourceTypeOCI {
		if fsOpts.Dockerfile != "" {
			return fmt.Errorf("a dockerfile cannot be set when the source type is %q", kraftfile.SourceTypeOCI)
		}
		return nil
	}

	if root != "" {
		fsOpts.Path = filepath.Join(root, fsOpts.Path)
	}

	if fsOpts.Dockerfile != "" {
		if fsOpts.Type != "" && fsOpts.Type != kraftfile.SourceTypeDockerfile {
			return fmt.Errorf("source type must be %q when a dockerfile is set, got %q", kraftfile.SourceTypeDockerfile, fsOpts.Type)
		}
		fsOpts.Type = kraftfile.SourceTypeDockerfile
	}

	if fsOpts.Type == "" {
		typ, err := DetectSourceType(fsOpts.Path)
		if err != nil {
			return err
		}
		fsOpts.Type = typ
	}

	return nil
}

// defaultRomFormat fills in the ROM default format. An OCI source carries its
// own format, so leave it unset and let the source dictate it.
func defaultRomFormat(format kraftfile.FsType, typ kraftfile.SourceType) kraftfile.FsType {
	if format != "" || typ == kraftfile.SourceTypeOCI {
		return format
	}
	return kraftfile.FsTypeErofs
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
				Format: defaultRomFormat(rom.Format, rom.Type),
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

	// Sources that are already packaged carry their own format, so leave it
	// unset and let the branch that knows the source fill it in.
	if opts.Rootfs.Format == "" &&
		opts.Rootfs.Type != kraftfile.SourceTypeCpio &&
		opts.Rootfs.Type != kraftfile.SourceTypeErofs &&
		opts.Rootfs.Type != kraftfile.SourceTypeOCI {
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
	case kraftfile.SourceTypeOCI:
		return buildRootfsOCI(ctx, opts)
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

// buildRootfsOCI pulls an OCI image and builds a rootfs from it for each
// requested platform. Two kinds of images are supported: Regular OCI and
// Unikraft images
func buildRootfsOCI(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	access, err := images.Accessor(ctx)
	if err != nil {
		return nil, err
	}

	uri, err := imagespec.ParseURIDefault(opts.Rootfs.Path)
	if err != nil {
		return nil, fmt.Errorf("parsing rootfs image reference %q: %w", opts.Rootfs.Path, err)
	}

	imagePlatforms := getPlatforms(opts.Platform)
	wanted := make([]ocispec.Platform, 0, 2*len(opts.Platform))
	for i, p := range opts.Platform {
		wanted = append(wanted, p, imagePlatforms[i].Platform)
	}

	matcher := ignoringOSFeatures(platforms.Any(wanted...))
	loaded, err := access.LoadAll(ctx, uri, matcher)
	if err != nil {
		return nil, fmt.Errorf("pulling rootfs image %q: %w", opts.Rootfs.Path, err)
	}
	defer func() {
		for _, img := range loaded {
			_ = img.Close()
		}
	}()

	byPlatform := make(map[string]*imagespec.Image, len(loaded))
	for _, img := range loaded {
		if img.Image == nil {
			continue
		}
		byPlatform[platforms.Format(platforms.Normalize(img.Image.Platform))] = img
	}

	// Keyed by image identity: two platforms share a flattened filesystem only
	// when they resolve to the same loaded image.
	flattened := make(map[*imagespec.Image]fs.FS, len(loaded))

	var imgs []*imagespec.Image
	for i, p := range opts.Platform {
		src := byPlatform[platforms.Format(platforms.Normalize(p))]
		if src == nil {
			src = byPlatform[platforms.Format(imagePlatforms[i].Platform)]
		}
		if src == nil {
			// A single-platform image carries no manifest to match against, so
			// accept it only when there is no other platform to confuse it with.
			if len(loaded) == 1 && len(opts.Platform) == 1 {
				src = loaded[0]
			} else {
				return nil, fmt.Errorf("rootfs image %q does not contain platform %q", opts.Rootfs.Path, platforms.Format(p))
			}
		}

		cfg := buildImageConfig(opts)
		if src.Image != nil {
			cfg = applyConfigOverrides(src.Image.Config, opts)
		}

		if src.Initrd != nil {
			f, err := os.CreateTemp("", "unikraft-rootfs-*")
			if err != nil {
				return nil, fmt.Errorf("could not create temporary file: %w", err)
			}
			defer func() {
				if rerr != nil && f != nil {
					f.Close()
					os.Remove(f.Name())
				}
			}()

			rc, _, err := src.Initrd.Open(ctx)
			if err != nil {
				return nil, fmt.Errorf("opening rootfs layer: %w", err)
			}
			if _, err := io.Copy(f, rc); err != nil {
				rc.Close()
				return nil, fmt.Errorf("reading rootfs layer: %w", err)
			}
			if err := rc.Close(); err != nil {
				return nil, fmt.Errorf("closing rootfs layer: %w", err)
			}
			if err := syncFile(f); err != nil {
				return nil, err
			}

			format, err := detectPackagedFormat(f.Name())
			if err != nil {
				return nil, fmt.Errorf("inspecting initrd of rootfs image %q: %w", opts.Rootfs.Path, err)
			}
			if opts.Rootfs.Format != "" && opts.Rootfs.Format != format {
				return nil, fmt.Errorf("unsupported rootfs format mismatch: source is %s but requested format is %s", format, opts.Rootfs.Format)
			}

			if err := padFile(f, opts.Rootfs.Pad); err != nil {
				return nil, err
			}
			if err := syncFile(f); err != nil {
				return nil, err
			}

			imgs = append(imgs, imagespec.NewImage(
				imagespec.WithImageConfig(cfg),
				imagespec.WithPlatform(p),
				imagespec.WithInitrd(imagespec.NewTempOSFile(f)),
			))
			continue
		}

		format := cmp.Or(opts.Rootfs.Format, DefaultRootfsFormat(opts.Platform))

		srcFS, ok := flattened[src]
		if !ok {
			layers, err := os.CreateTemp("", "unikraft-buildkit-*.tar")
			if err != nil {
				return nil, fmt.Errorf("could not create temporary file: %w", err)
			}
			defer func() {
				layers.Close()
				os.Remove(layers.Name())
			}()

			if err := flattenImageLayers(ctx, opts, src, uri, layers); err != nil {
				return nil, err
			}

			srcFS, err = buildfs.TarballFS(layers)
			if err != nil {
				return nil, fmt.Errorf("could not open flattened rootfs image as filesystem: %w", err)
			}
			flattened[src] = srcFS
		}

		f, err := os.CreateTemp("", "unikraft-rootfs-*."+string(format))
		if err != nil {
			return nil, fmt.Errorf("could not create temporary file: %w", err)
		}
		defer func() {
			if rerr != nil && f != nil {
				f.Close()
				os.Remove(f.Name())
			}
		}()

		if err := packageFS(ctx, format, f, srcFS, opts.Rootfs); err != nil {
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

// flattenImageLayers writes the flattened filesystem of a regular OCI image to
// dst as an uncompressed tarball, using BuildKit to do the flattening.
func flattenImageLayers(ctx context.Context, opts BuildOpts, src *imagespec.Image, uri *imagespec.URI, dst *os.File) error {
	if uri.Scheme != imagespec.URISchemeOCI {
		return fmt.Errorf("rootfs image %q must be a registry reference, %q is not supported", uri.Path, uri.Scheme)
	}

	if src.Image == nil {
		return fmt.Errorf("rootfs image %q has no config to take a platform from", uri.Path)
	}

	ref := uri.Path
	if src.Descriptor.Digest != "" && !strings.Contains(ref, "@") {
		ref += "@" + src.Descriptor.Digest.String()
	}

	imagePlatform := getPlatform(src.Image.Platform).Platform

	imageOpts := []llb.ImageOption{llb.Platform(imagePlatform)}
	constraints := []llb.ConstraintsOpt{llb.Platform(imagePlatform)}
	if opts.NoCache {
		imageOpts = append(imageOpts, llb.ResolveModeForcePull)
		constraints = append(constraints, llb.IgnoreCache)
	}

	def, err := llb.Image(ref, imageOpts...).Marshal(ctx, constraints...)
	if err != nil {
		return fmt.Errorf("marshalling rootfs image source: %w", err)
	}

	session, err := buildkitSession(ctx)
	if err != nil {
		return err
	}

	err = solveToTar(ctx, dst, client.SolveOpt{Session: session},
		func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
			return c.Solve(ctx, gateway.SolveRequest{
				Definition: def.ToPB(),
				Evaluate:   true,
			})
		})
	if err != nil {
		return fmt.Errorf("flattening rootfs image %q: %w", uri.Path, err)
	}

	return nil
}

func buildRootfsDockerfile(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	session, err := buildkitSession(ctx)
	if err != nil {
		return nil, err
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

		imgs = append(imgs, imagespec.NewImage(
			imagespec.WithImageConfig(applyConfigOverrides(config.Config, opts)),
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

	if err := padFile(destFS, opts.Pad); err != nil {
		return err
	}

	return syncFile(destFS)
}

// padFile pads f up to a multiple of pad bytes. A pad of zero does nothing.
func padFile(f *os.File, pad int64) error {
	if pad <= 0 {
		return nil
	}

	pos, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("could not seek to end of file: %w", err)
	}
	if rem := pos % pad; rem != 0 {
		padding := make([]byte, pad-rem)
		if _, err := f.Write(padding); err != nil {
			return fmt.Errorf("could not pad file to page alignment: %w", err)
		}
	}

	return nil
}

// syncFile flushes f to disk.
func syncFile(f *os.File) error {
	if err := f.Sync(); err != nil {
		return fmt.Errorf("could not sync file: %w", err)
	}
	return nil
}

// buildkitSession returns the session attachables every solve needs, which is
// the registry auth wired up from both the docker config and the current
// profile.
func buildkitSession(ctx context.Context) ([]session.Attachable, error) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	dockerConfig := dockerconfig.LoadDefaultConfigFile(os.Stderr)

	return []session.Attachable{
		authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
			AuthConfigProvider: images.LoadBuildkitAuthConfig(dockerConfig, profile),
		}),
	}, nil
}

// solveToTar runs build against BuildKit, exporting an uncompressed tarball of
// the result to dst, and waits for the progress writer to drain.
func solveToTar(ctx context.Context, dst *os.File, solveOpt client.SolveOpt, build gateway.BuildFunc) error {
	solveOpt.Ref = identity.NewID()
	solveOpt.Exports = []client.ExportEntry{{
		Type: client.ExporterTar,
		Output: func(map[string]string) (io.WriteCloser, error) {
			return ukio.NopWriteCloser(dst), nil
		},
	}}

	c, cleanup, err := buildkit.ConnectToBuildkit(ctx)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	pw, err := progresswriter.NewPrinter(context.WithoutCancel(ctx), os.Stderr, "auto")
	if err != nil {
		return err
	}

	if _, err := c.Build(ctx, solveOpt, "buildctl", build, pw.Status()); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-pw.Done():
	}
	if pw.Err() != nil {
		return pw.Err()
	}

	if err := dst.Sync(); err != nil {
		return fmt.Errorf("could not sync tarball: %w", err)
	}
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("could not rewind tarball: %w", err)
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

// getPlatform maps a unikraft target platform onto the linux platform BuildKit
// builds for.
func getPlatform(p ocispec.Platform) exptypes.Platform {
	p.OS = "linux"
	p.OSFeatures = nil
	p.OSVersion = ""
	p = platforms.Normalize(p)
	return exptypes.Platform{
		ID:       platforms.Format(p),
		Platform: p,
	}
}

func getPlatforms(ps []ocispec.Platform) (exp []exptypes.Platform) {
	for _, p := range ps {
		exp = append(exp, getPlatform(p))
	}
	return exp
}
