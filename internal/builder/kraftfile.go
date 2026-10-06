// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"unikraft.com/x/kraftfile"
)

func KraftfileToBuildOpts(dir string, kf *kraftfile.Kraftfile) (BuildOpts, error) {
	var opts BuildOpts

	opts.Cmd = []string(kf.Cmd)
	opts.Env = kf.Env
	opts.Labels = kf.Labels

	if kf.Runtime != nil {
		opts.Runtime = string(*kf.Runtime)
	}

	if kf.Unikraft != nil {
		if kf.Unikraft.Type != kraftfile.UnikraftTypeKernel {
			return BuildOpts{}, fmt.Errorf("building unikraft from source not currently supported")
		}
		if opts.Runtime != "" {
			return BuildOpts{}, fmt.Errorf("kernel and runtime configuration are mutually exclusive")
		}
		if kf.Unikraft.Source == "" {
			return BuildOpts{}, fmt.Errorf("kernel entry is missing a source path")
		}
		if kf.Unikraft.Version != "" {
			return BuildOpts{}, fmt.Errorf("kernel entry does not support a version")
		}
		opts.Kernel = filepath.Join(dir, kf.Unikraft.Source)
	}

	if kf.Libraries != nil {
		// these are the same build process as kf.Unikraft
		return BuildOpts{}, fmt.Errorf("library configuration not currently supported")
	}
	if kf.Volumes != nil {
		return BuildOpts{}, fmt.Errorf("volumes configuration not currently supported")
	}
	if kf.Template != nil {
		return BuildOpts{}, fmt.Errorf("template configuration not currently supported")
	}

	for _, target := range kf.Targets {
		kconfig := target.KConfig
		if kf.Unikraft != nil {
			kconfig = slices.Concat(kf.Unikraft.KConfig, kconfig)
		}
		features := make([]string, 0, len(kconfig))
		for _, kv := range kconfig {
			features = append(features, fmt.Sprintf("%s=%v", kv.Key, kv.Value))
		}
		var version string
		if value, ok := kconfig.Lookup("CONFIG_UK_FULLVERSION"); ok {
			version = fmt.Sprint(value)
		}
		opts.Platform = append(opts.Platform, ocispec.Platform{
			Architecture: target.Arch,
			OS:           target.Plat,
			OSVersion:    version,
			OSFeatures:   features,
		})
	}

	for _, rom := range kf.Roms {
		if rom.Source == nil || rom.Source.Path == "" {
			return BuildOpts{}, fmt.Errorf("rom entry is missing a source path")
		}
		romFormat := cmp.Or(rom.Format, kraftfile.FsTypeErofs)
		romOpt := FSOpts{
			Path:   rom.Source.Path,
			Format: romFormat,
			Type:   rom.Source.Type,
			// Pad the file to page-size alignment. This is required by the platform
			// which rejects ROM files that are not page-aligned.
			Pad: 4096,
		}
		if err := resolveSource(dir, &romOpt); err != nil {
			return BuildOpts{}, fmt.Errorf("resolving rom source %q: %w", rom.Source.Path, err)
		}
		opts.Roms = append(opts.Roms, romOpt)
	}

	if kf.Rootfs != nil {
		if kf.Rootfs.Source == nil || kf.Rootfs.Source.Path == "" {
			return BuildOpts{}, fmt.Errorf("rootfs entry is missing a source path")
		}
		opts.Rootfs.Path = kf.Rootfs.Source.Path
		opts.Rootfs.Format = kf.Rootfs.Format
		opts.Rootfs.Type = kf.Rootfs.Source.Type
		opts.Rootfs.Dockerfile = kf.Rootfs.Source.Dockerfile
		if err := resolveSource(dir, &opts.Rootfs); err != nil {
			return BuildOpts{}, fmt.Errorf("resolving rootfs source %q: %w", kf.Rootfs.Source.Path, err)
		}
	}

	return opts, nil
}
