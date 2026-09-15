// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"
)

// DefaultDockerfileName is the file that a project directory without a
// Kraftfile must contain to be built.
const DefaultDockerfileName = "Dockerfile"

// kraftfileNames are the Kraftfile names that a project directory can contain,
// in order of precedence. The order is the name order of the directory listing.
var kraftfileNames = []string{
	"Kraftfile",
	"Kraftfile.yaml",
	"Kraftfile.yml",
	"kraft.yaml",
	"kraft.yml",
}

// LoadBuildOpts resolves path to a Kraftfile or a Dockerfile and returns the
// build options. Path is a project directory, a Kraftfile, or a Dockerfile.
func LoadBuildOpts(ctx context.Context, path string) (BuildOpts, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BuildOpts{}, fmt.Errorf("build input %q does not exist", path)
		}
		return BuildOpts{}, fmt.Errorf("checking build input %q: %w", path, err)
	}

	if !fi.IsDir() {
		if isDockerfileName(filepath.Base(path)) {
			return DockerfileToBuildOpts(path), nil
		}
		return loadKraftfile(ctx, path, filepath.Dir(path))
	}

	for _, name := range kraftfileNames {
		kfPath := filepath.Join(path, name)
		if fi, err := os.Stat(kfPath); err == nil && !fi.IsDir() {
			return loadKraftfile(ctx, kfPath, path)
		}
	}

	dockerfilePath := filepath.Join(path, DefaultDockerfileName)
	if fi, err := os.Stat(dockerfilePath); err == nil && !fi.IsDir() {
		return DockerfileToBuildOpts(dockerfilePath), nil
	}

	return BuildOpts{}, fmt.Errorf("no Kraftfile or %s found in %q", DefaultDockerfileName, path)
}

// loadKraftfile parses the Kraftfile at path. Relative source paths in the
// Kraftfile resolve against dir.
func loadKraftfile(ctx context.Context, path, dir string) (BuildOpts, error) {
	kf, err := kraftfile.ParseFile(path, kraftfile.WithSkippedVersionCheck())
	if err != nil {
		return BuildOpts{}, err
	}
	if semver.Compare(kf.Spec, kraftfile.SpecVersionMin) < 0 {
		log.G(ctx).Warn().
			Str("spec", kf.Spec).
			Str("min", kraftfile.SpecVersionMin).
			Msg("Kraftfile spec version is older than minimum; parsing is best-effort")
	} else if semver.Compare(kf.Spec, kraftfile.SpecVersionMax) > 0 {
		log.G(ctx).Warn().
			Str("spec", kf.Spec).
			Str("max", kraftfile.SpecVersionMax).
			Msg("Kraftfile spec version is newer than maximum; parsing is best-effort")
	}
	return KraftfileToBuildOpts(dir, kf)
}

// DockerfileToBuildOpts returns build options that use the Dockerfile at path
// as the only rootfs source. The directory of the Dockerfile is the context.
func DockerfileToBuildOpts(path string) BuildOpts {
	return BuildOpts{
		Rootfs: FSOpts{
			Path: path,
			Type: kraftfile.SourceTypeDockerfile,
		},
	}
}

// isDockerfileName reports whether a file name denotes a Dockerfile, such as
// "Dockerfile", "app.Dockerfile" or "Dockerfile.dev".
func isDockerfileName(base string) bool {
	return base == DefaultDockerfileName ||
		slices.Contains(strings.Split(base, "."), DefaultDockerfileName)
}
