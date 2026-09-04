// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
)

// Busybox is a busybox rootfs with no application. Tests give their own
// command with --args or runtime.args, which replaces the cmd of the image.
var Busybox = &SharedImage{
	Name: "busybox-e2e",
	Files: map[string]string{
		"Dockerfile": "FROM busybox:latest",
		"Kraftfile": `spec: v0.7
name: busybox-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["sh", "-c"]
`,
	},
}

// Image is a build context that a test builds into the registry.
type Image struct {
	// Name is the image name, without the organization and the tag.
	Name string
	// Each key is a file name in the context root.
	Files map[string]string
}

// Ref gives the full reference of the image for tag.
func (i *Image) Ref(env *TestEnv, tag string) string {
	return env.Config.Profile.Organization + "/" + i.Name + ":" + tag
}

// Build writes the files to a temporary context and builds the image at ref.
// The sandbox of the test deletes the image, unless opts give WithNoSandbox.
func (i *Image) Build(t *testing.T, env *TestEnv, ref string, opts ...CmdOption) error {
	t.Helper()

	dir, err := os.MkdirTemp("", "unikraft-test-image-*")
	if err != nil {
		return fmt.Errorf("creating build context of %s: %w", ref, err)
	}
	defer os.RemoveAll(dir)

	for name, content := range i.Files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("populating build context of %s: %w", ref, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("populating build context of %s: %w", ref, err)
		}
	}

	opts = append([]CmdOption{WithWorkDir(dir)}, opts...)
	if out, err := env.RunRaw(t, []string{"unikraft", "build", ".", "--output", ref}, opts...); err != nil {
		return fmt.Errorf("building image %s: %w\n%s", ref, err, out)
	}

	return nil
}

// SharedImage is an image that tests use. Build makes the image one time,
// and CleanupSharedImages deletes it when the test binary ends.
type SharedImage struct {
	Image

	once sync.Once
	ref  string
	err  error
}

var (
	sharedImageTag    = randomImageTag()
	sharedImageMu     sync.Mutex
	sharedImageRefs   []string
	sharedImageConfig *config.Config
)

func randomImageTag() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "shared-" + hex.EncodeToString(b[:])
}

// Build makes the image one time and gives the full reference. Later calls
// give the reference of the first build.
func (s *SharedImage) Build(t *testing.T, env *TestEnv) string {
	t.Helper()
	require.NotNil(t, env.Config, "shared image build requires a config")
	s.once.Do(func() {
		s.ref, s.err = s.build(t, env)
	})
	require.NoError(t, s.err)
	return s.ref
}

// build makes the image out of the sandbox of the test, so that the cleanup of
// one test does not delete an image that other tests still use.
func (s *SharedImage) build(t *testing.T, env *TestEnv) (string, error) {
	t.Helper()
	ref := s.Ref(env, sharedImageTag)
	if err := s.Image.Build(t, env, ref, WithNoSandbox(), WithoutCancel()); err != nil {
		return "", err
	}
	registerSharedImage(env, ref)
	return ref, nil
}

// registerSharedImage records ref for deletion after the tests end.
func registerSharedImage(env *TestEnv, ref string) {
	sharedImageMu.Lock()
	defer sharedImageMu.Unlock()

	if sharedImageConfig == nil {
		sharedImageConfig = env.Config.Config
	}
	sharedImageRefs = append(sharedImageRefs, ref)
}

// CleanupSharedImages deletes every shared image from the registry.
func CleanupSharedImages() {
	sharedImageMu.Lock()
	cfg := sharedImageConfig
	refs := slices.Clone(sharedImageRefs)
	sharedImageRefs = nil
	sharedImageMu.Unlock()

	if cfg == nil || len(refs) == 0 {
		return
	}

	ctx := config.WithConfig(context.Background(), cfg)
	access, err := images.Accessor(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to cleanup shared images: %v\n", err)
		return
	}

	for _, ref := range refs {
		uri, err := imagespec.GuessURI(ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse shared image %s: %v\n", ref, err)
			continue
		}
		if err := access.Delete(ctx, uri); err != nil {
			fmt.Fprintf(os.Stderr, "failed to delete shared image %s: %v\n", ref, err)
		}
	}
}
