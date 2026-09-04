// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/builder/buildflags"
)

type BuildOpts struct {
	Rootfs FSOpts
	Roms   []FSOpts

	Runtime string

	Platform []ocispec.Platform

	Cmd    []string
	Env    kraftfile.Map
	Labels map[string]string

	// Buildkit params
	BuildArg      []string
	Target        string
	BuildContexts map[string]string
	Secrets       []*buildflags.Secret
	SSH           []*buildflags.SSH

	NoCache bool
}

type FSOpts struct {
	Path       string
	Type       kraftfile.SourceType
	Dockerfile string

	// Output params
	Format     kraftfile.FsType
	Pad        int64 // pad file to specified size
	Compress   bool
	KeepOwners bool
}

func (o *BuildOpts) FilterArch(arches ...string) error {
	if len(arches) == 0 {
		return nil
	}
	if len(o.Platform) == 0 {
		for _, arch := range arches {
			if slices.ContainsFunc(o.Platform, func(p ocispec.Platform) bool {
				return p.Architecture == arch
			}) {
				continue
			}
			o.Platform = append(o.Platform, ocispec.Platform{
				Architecture: arch, OS: imagespec.PlatformKraftcloud,
			})
		}
		return nil
	}

	available := make([]string, 0, len(o.Platform))
	for _, p := range o.Platform {
		available = append(available, platforms.Format(p))
	}
	o.Platform = slices.DeleteFunc(o.Platform, func(p ocispec.Platform) bool {
		norm, err := imagespec.NormalizeArch(p.Architecture)
		return err != nil || !slices.Contains(arches, norm)
	})
	if len(o.Platform) == 0 {
		return fmt.Errorf("no target with architecture %s: targets are %s",
			strings.Join(arches, ", "), strings.Join(available, ", "))
	}
	return nil
}

// erofsFeatureFlags are the OSFeatures that signal EROFS support in a runtime.
var erofsFeatureFlags = []string{
	"CONFIG_LIBUKFS_EROFS=y",
	"CONFIG_EROFS_FS=y",
}

// runtimeHasErofsSupport reports whether the given runtime's OSFeatures
// indicate EROFS support.
func runtimeHasErofsSupport(p ocispec.Platform) bool {
	for _, f := range p.OSFeatures {
		for _, e := range erofsFeatureFlags {
			if strings.EqualFold(f, e) {
				return true
			}
		}
	}
	return false
}

// DefaultRootfsFormat returns the default rootfs format based on the runtime
// platforms' features. If all platforms advertise EROFS support, erofs is
// returned.
func DefaultRootfsFormat(ps []ocispec.Platform) kraftfile.FsType {
	if len(ps) == 0 {
		return kraftfile.FsTypeCpio
	}
	for _, p := range ps {
		if !runtimeHasErofsSupport(p) {
			return kraftfile.FsTypeCpio
		}
	}
	return kraftfile.FsTypeErofs
}

// Build a unikraft image based on the provided build options.
func Build(ctx context.Context, opts BuildOpts) ([]*imagespec.Image, error) {
	if opts.Runtime == "" && opts.Rootfs.Path == "" && len(opts.Roms) == 0 {
		return nil, fmt.Errorf("no runtime, rootfs, or roms specified: nothing to build")
	}

	meta := imagespec.ImageMetadata{
		Created: new(time.Now()),
	}

	// Build kernel if a runtime is specified. This also determines the
	// set of platforms we're building for.
	var kernels []*imagespec.Image
	if opts.Runtime != "" {
		var err error
		kernels, err = BuildKernel(ctx, opts)
		if err != nil {
			return nil, err
		}
		defer func() {
			for _, kernel := range kernels {
				kernel.Close()
			}
		}()
		opts.Platform = make([]ocispec.Platform, 0, len(kernels))
		for _, kernel := range kernels {
			opts.Platform = append(opts.Platform, kernel.Image.Platform)
		}
	}

	if len(opts.Platform) == 0 {
		return nil, fmt.Errorf("no platform targets specified")
	}

	// Build ROMs if any are specified.
	romFiles, err := BuildRoms(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, perPlat := range romFiles {
			for _, f := range perPlat {
				if f != nil {
					_ = f.Cleanup()
				}
			}
		}
	}()

	// Build rootfs if a path is specified.
	var roots []*imagespec.Image
	if opts.Rootfs.Path != "" {
		var err error
		roots, err = BuildRootfs(ctx, opts)
		if err != nil {
			return nil, err
		}
		defer func() {
			for _, root := range roots {
				root.Close()
			}
		}()
	}

	// When we have both kernels and rootfs, they must match 1:1 by platform.
	if len(kernels) > 0 && len(roots) > 0 && len(kernels) != len(roots) {
		log.G(ctx).
			Fatal().
			Int("kernels", len(kernels)).
			Int("roots", len(roots)).
			Msg("internal error: number of kernels does not match number of root filesystems")
	}

	// Assemble images, one per platform.
	images := make([]*imagespec.Image, 0, len(opts.Platform))
	for i, p := range opts.Platform {
		pID := platforms.Format(p)
		imgOpts := []imagespec.NewImageOpt{
			imagespec.WithImageMetadata(meta),
			imagespec.WithPlatform(p),
		}

		// Attach kernel if available.
		if i < len(kernels) {
			kernelPlatformID := platforms.Format(kernels[i].Image.Platform)
			if kernelPlatformID != pID {
				log.G(ctx).
					Fatal().
					Str("got", kernelPlatformID).
					Str("expected", pID).
					Msg("internal error: kernel platform does not match expected platform")
			}
			imgOpts = append(imgOpts, imagespec.WithKernel(kernels[i].Kernel))
			kernels[i].Kernel = nil
		}

		// Attach rootfs/initrd and use its config if available.
		cfg := buildImageConfig(opts)
		if i < len(roots) {
			rootfsPlatformID := platforms.Format(roots[i].Image.Platform)
			if rootfsPlatformID != pID {
				log.G(ctx).
					Fatal().
					Str("got", rootfsPlatformID).
					Str("expected", pID).
					Msg("internal error: rootfs platform does not match expected platform")
			}
			imgOpts = append(imgOpts, imagespec.WithInitrd(roots[i].Initrd))
			roots[i].Initrd = nil
			// The rootfs build may have produced a richer config (e.g. from
			// a Dockerfile). Use it as the base and layer our overrides on top.
			cfg = roots[i].Image.Config
			if opts.Cmd != nil {
				cfg.Cmd = opts.Cmd
			}
			if opts.Env != nil {
				env := make([]string, 0, len(opts.Env))
				for _, kv := range opts.Env {
					env = append(env, fmt.Sprintf("%s=%s", kv.Key, kv.Value))
				}
				cfg.Env = append(env, cfg.Env...)
			}
			cfg.Labels = opts.Labels
		}
		imgOpts = append(imgOpts, imagespec.WithImageConfig(cfg))

		// Attach roms if available. A ROM payload is a compiled artifact, so
		// each image gets the ROM that was built for its own platform.
		for j := range romFiles {
			if i >= len(romFiles[j]) || romFiles[j][i] == nil {
				log.G(ctx).
					Fatal().
					Int("rom", j).
					Str("platform", pID).
					Msg("internal error: no rom built for this platform")
			}
			imgOpts = append(imgOpts, imagespec.WithRom(romFiles[j][i]))
			romFiles[j][i] = nil // ownership moves to the image
		}

		images = append(images, imagespec.NewImage(imgOpts...))
	}

	return images, nil
}
