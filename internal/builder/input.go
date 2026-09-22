// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"errors"
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

	// A Dockerfile is not a Kraftfile document. The name must select it before
	// the Kraftfile parser reads the file.
	if !fi.IsDir() && slices.Contains(strings.Split(filepath.Base(path), "."), DefaultDockerfileName) {
		return BuildOpts{
			Rootfs: FSOpts{
				Path: path,
				Type: kraftfile.SourceTypeDockerfile,
			},
		}, nil
	}

	kf, err := kraftfile.ParseDirectory(path, kraftfile.WithSkippedVersionCheck())
	if errors.Is(err, kraftfile.ErrNoKraftfile) {
		// Only a directory gives this error, thus the Dockerfile is in path.
		dockerfilePath := filepath.Join(path, DefaultDockerfileName)
		if fi, err := os.Stat(dockerfilePath); err == nil && !fi.IsDir() {
			return BuildOpts{
				Rootfs: FSOpts{
					Path: dockerfilePath,
					Type: kraftfile.SourceTypeDockerfile,
				},
			}, nil
		}
		return BuildOpts{}, fmt.Errorf("no Kraftfile or %s found in %q", DefaultDockerfileName, path)
	} else if err != nil {
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

	// Relative source paths in the Kraftfile resolve against its directory.
	dir := path
	if !fi.IsDir() {
		dir = filepath.Dir(path)
	}
	return KraftfileToBuildOpts(dir, kf)
}
