// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/tonistiigi/fsutil"
	buildapi "unikraft.com/cloud/sdk/build"
	"unikraft.com/x/build/buildflags"
	"unikraft.com/x/build/progress"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/image-spec/reference"
	"unikraft.com/x/image-spec/schemes"
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
)

// remoteContext is the name the session serves the build context under.
const remoteContext = "context"

// runRemote builds on the build service at c.Remote, which pushes the image.
func (c *ImageBuildCmd) runRemote(ctx context.Context, cfg *config.Config, partition *resource.Partition) error {
	secrets, err := buildflags.ParseSecretSpecs(c.Secret)
	if err != nil {
		return err
	}
	ssh, err := buildflags.ParseSSHSpecs(c.SSH)
	if err != nil {
		return err
	}
	req, dir, err := c.remoteRequest(secrets, ssh)
	if err != nil {
		return err
	}

	profile, err := cfg.CurrentProfile()
	if err != nil {
		return err
	}
	client, err := buildapi.New(c.Remote, buildapi.WithToken(profile.Token))
	if err != nil {
		return err
	}
	log.G(ctx).Info().Str("url", c.Remote).Msg("building remotely")

	s, err := remoteSession(ctx, dir, secrets, ssh)
	if err != nil {
		return err
	}
	defer s.Close()

	disp, err := progress.NewDisplay(ctx, os.Stderr, progressui.AutoMode)
	if err != nil {
		return err
	}
	err = followRemote(ctx, client, req, s, disp)
	if cerr := disp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}

	if partition != nil {
		if err := addImageToPartition(ctx, partition, *req.Output[0].Image); err != nil {
			return fmt.Errorf("adding built image to partition: %w", err)
		}
	}
	return nil
}

// followRemote creates the build, attaches s to it and draws its progress until it ends.
func followRemote(ctx context.Context, client *buildapi.Client, req buildapi.CreateBuildRequest, s *session.Session, disp *progress.Display) error {
	b, err := client.CreateBuild(ctx, req)
	if err != nil {
		return err
	}
	if b.Session != nil {
		if err := client.Attach(ctx, b.ID, s); err != nil {
			return errors.Join(err, cancelRemote(ctx, client, b.ID))
		}
	}

	for ev, err := range client.Logs(ctx, b.ID) {
		if err != nil {
			if ctx.Err() != nil {
				return errors.Join(err, cancelRemote(ctx, client, b.ID))
			}
			return err
		}
		if ev.SolveStatus != nil {
			disp.Write(ev.SolveStatus)
		}
		if ev.Error != nil {
			return ev.Error
		}
		if ev.Result != nil {
			for _, img := range ev.Result.Images {
				log.G(ctx).Info().Str("output", img.Ref).Str("digest", img.Digest).Msg("pushed image")
			}
		}
	}
	return nil
}

// cancelRemote cancels the remote build id, also once ctx has ended, waiting at most 10 seconds.
func cancelRemote(ctx context.Context, client *buildapi.Client, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_, err := client.CancelBuild(ctx, id)
	return err
}

// remoteRequest returns the request for a remote build of c, and the directory its session serves.
func (c *ImageBuildCmd) remoteRequest(secrets []*buildflags.Secret, ssh []*buildflags.SSH) (buildapi.CreateBuildRequest, string, error) {
	if c.Insecure != nil {
		return buildapi.CreateBuildRequest{}, "", errors.New("--insecure does not apply to a remote build")
	}
	u, err := url.Parse(c.Remote)
	if err != nil {
		return buildapi.CreateBuildRequest{}, "", err
	}
	// The service gets the profile's token, the project and its secrets.
	if u.Scheme == "http" {
		return buildapi.CreateBuildRequest{}, "", fmt.Errorf("the build service %s must be https", c.Remote)
	}
	if c.Output == "" {
		return buildapi.CreateBuildRequest{}, "", errors.New("a remote build needs --output, the image to push")
	}
	output, err := imagespec.GuessLocation(c.Output)
	if err != nil {
		return buildapi.CreateBuildRequest{}, "", err
	}
	if output.Scheme != schemes.OCI {
		return buildapi.CreateBuildRequest{}, "", fmt.Errorf("a remote build pushes to a registry, not to %s", output)
	}
	ref, err := reference.Parse(output.Path, reference.WithDefaultDomain("index.unikraft.io"))
	if err != nil {
		return buildapi.CreateBuildRequest{}, "", err
	}

	fi, err := os.Stat(c.Input)
	if err != nil {
		return buildapi.CreateBuildRequest{}, "", err
	}
	dir, name := c.Input, ""
	if fi.IsDir() {
		for _, n := range kraftfile.DefaultFileNames {
			if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
				name = n
				break
			}
		}
	} else {
		dir, name = filepath.Dir(c.Input), filepath.Base(c.Input)
	}

	src := buildapi.SessionSource(remoteContext)
	req := buildapi.CreateBuildRequest{
		Contexts:   buildapi.BuildContexts{Context: src},
		Platforms:  c.Arch,
		NoCache:    new(c.NoCache),
		Registries: map[string]buildapi.Registry{"*": buildapi.SessionRegistry()},
		Output:     []buildapi.BuildOutput{buildapi.Image(ref.String())},
	}
	switch {
	case name == "": // a directory without a Kraftfile builds its Dockerfile
	case slices.Contains(strings.Split(name, "."), "Dockerfile"): // as x/build tells one

		req.Contexts.Dockerfile = new(src.WithFilename(name))
	default:
		req.Kraftfile = buildapi.KraftfileFrom(src.WithFilename(name))
	}

	for _, arg := range c.BuildArg {
		key, val, ok := strings.Cut(arg, "=")
		if key == "" {
			return buildapi.CreateBuildRequest{}, "", fmt.Errorf("invalid build-arg %q", arg)
		}
		if !ok {
			val, _ = os.LookupEnv(key)
		}
		if req.BuildArgs == nil {
			req.BuildArgs = map[string]string{}
		}
		req.BuildArgs[key] = val
	}

	for _, secret := range secrets {
		if req.Secrets == nil {
			req.Secrets = map[string]buildapi.Secret{}
		}
		req.Secrets[secret.ID] = buildapi.SessionSecret(secret.ID)
	}

	for _, agent := range ssh {
		if req.SSH == nil {
			req.SSH = map[string]buildapi.SSHAgent{}
		}
		req.SSH[agent.ID] = buildapi.SSHAgent{Session: agent.ID}
	}

	return req, dir, nil
}

// remoteSession serves dir, the registry logins, secrets and ssh to a remote build.
func remoteSession(ctx context.Context, dir string, secrets []*buildflags.Secret, ssh []*buildflags.SSH) (*session.Session, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	// The key keeps filesync incremental across builds of dir without sending its path.
	sum := sha256.Sum256([]byte(abs))
	s, err := session.NewSession(ctx, hex.EncodeToString(sum[:]))
	if err != nil {
		return nil, err
	}

	fs, err := fsutil.NewFS(abs)
	if err != nil {
		return nil, err
	}
	s.Allow(filesync.NewFSSyncProvider(filesync.StaticDirSource{remoteContext: fs}))

	auth, err := authAttachable(ctx)
	if err != nil {
		return nil, err
	}
	s.Allow(auth)

	if len(secrets) > 0 {
		a, err := buildflags.CreateSecrets(secrets)
		if err != nil {
			return nil, err
		}
		s.Allow(a)
	}
	if len(ssh) > 0 {
		a, err := buildflags.CreateSSH(ssh)
		if err != nil {
			return nil, err
		}
		s.Allow(a)
	}

	return s, nil
}
