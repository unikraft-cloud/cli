// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"fmt"
	"io"
	"os"

	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/progress/progresswriter"

	"unikraft.com/cli/internal/buildkit"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
	ukio "unikraft.com/x/io"
)

// solver holds one BuildKit connection for several solves.
type solver struct {
	client  *client.Client
	cleanup func()
	session []session.Attachable
}

func newSolver(ctx context.Context) (*solver, error) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	dockerConfig := dockerconfig.LoadDefaultConfigFile(os.Stderr)

	c, cleanup, err := buildkit.ConnectToBuildkit(ctx)
	if err != nil {
		return nil, err
	}

	return &solver{
		client:  c,
		cleanup: cleanup,
		session: []session.Attachable{
			authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
				AuthConfigProvider: images.LoadBuildkitAuthConfig(dockerConfig, profile),
			}),
		},
	}, nil
}

func (s *solver) close() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

// solveToTar runs build, exporting an uncompressed tarball of the result to
// dst, which is left synced and rewound. Progress is printed under prefix.
func (s *solver) solveToTar(ctx context.Context, dst *os.File, prefix string, solveOpt client.SolveOpt, build gateway.BuildFunc) error {
	solveOpt.Ref = identity.NewID()
	solveOpt.Session = s.session
	solveOpt.Exports = []client.ExportEntry{{
		Type: client.ExporterTar,
		Output: func(map[string]string) (io.WriteCloser, error) {
			return ukio.NopWriteCloser(dst), nil
		},
	}}

	pw, err := progresswriter.NewPrinter(context.WithoutCancel(ctx), os.Stderr, "auto")
	if err != nil {
		return err
	}
	w := progresswriter.NewMultiWriter(pw).WithPrefix(prefix, true)

	if _, err := s.client.Build(ctx, solveOpt, "buildctl", build, w.Status()); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-pw.Done():
	}
	if err := pw.Err(); err != nil {
		return err
	}

	if err := dst.Sync(); err != nil {
		return fmt.Errorf("could not sync tarball: %w", err)
	}
	if _, err := dst.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("could not rewind tarball: %w", err)
	}

	return nil
}
