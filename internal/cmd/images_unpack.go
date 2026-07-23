// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/containerd/platforms"
	goerofs "github.com/unikraft/go-archivefs/erofs"
	gocpio "github.com/unikraft/go-cpio"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/images"
)

// ImagesUnpackCmd downloads an image from a source reference and unpacks its
// rootfs (initrd) into a directory. It is intentionally hidden from the help
// output while the feature is still experimental.
type ImagesUnpackCmd struct {
	Source   string   `arg:"" help:"Source image reference (registry, OCI archive, or OCI layout directory)."`
	Dest     string   `arg:"" help:"Destination directory to extract the rootfs into."`
	Insecure []string `help:"Allow insecure (HTTP/unverified TLS) connections to registries. Specify hostnames to restrict, or omit to apply to all." type:"optional"`
}

func (cmd ImagesUnpackCmd) Run(ctx context.Context) error {
	var opts []images.AccessorOpt
	if cmd.Insecure != nil {
		if len(cmd.Insecure) > 0 {
			opts = append(opts, images.WithInsecureRegistry(cmd.Insecure...))
		} else {
			opts = append(opts, images.WithInsecureRegistries())
		}
	}

	access, err := images.Accessor(ctx, opts...)
	if err != nil {
		return err
	}

	src, err := imagespec.GuessURI(cmd.Source)
	if err != nil {
		return fmt.Errorf("parsing source image reference: %w", err)
	}

	imgs, err := access.LoadAll(ctx, src, platforms.All)
	if err != nil {
		return fmt.Errorf("loading image from source: %w", err)
	}
	defer func() {
		for _, img := range imgs {
			img.Close()
		}
	}()

	if len(imgs) == 0 {
		return fmt.Errorf("no images found at %q", cmd.Source)
	}
	img := imgs[0]
	if img.Initrd == nil {
		return fmt.Errorf("image %q has no rootfs (initrd)", cmd.Source)
	}

	if err := os.MkdirAll(cmd.Dest, 0o755); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	rc, _, err := img.Initrd.Open(ctx)
	if err != nil {
		return fmt.Errorf("opening rootfs: %w", err)
	}
	defer rc.Close()

	br := bufio.NewReader(rc)
	need := goerofs.SuperBlockOffset + 4
	peeked, err := br.Peek(need)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return fmt.Errorf("reading rootfs: %w", err)
	}

	switch {
	case len(peeked) >= 6 && gocpio.IsValid(bytes.NewReader(peeked)):
		// CPIO can be unpacked straight from the stream.
		if err := gocpio.Unpack(br, cmd.Dest); err != nil {
			return fmt.Errorf("unpacking rootfs (CPIO): %w", err)
		}
	case len(peeked) >= need &&
		goerofs.IsValid(bytes.NewReader(peeked[goerofs.SuperBlockOffset:])):
		// EROFS needs random access, so spill the stream to a temp file first.
		if err := unpackEROFSStream(br, cmd.Dest); err != nil {
			return fmt.Errorf("unpacking rootfs (EROFS): %w", err)
		}
	default:
		return fmt.Errorf("unsupported rootfs format (expected CPIO or EROFS)")
	}

	log.G(ctx).Info().
		Str("source", cmd.Source).
		Str("dest", cmd.Dest).
		Msg("unpacked rootfs")
	return nil
}

// unpackEROFSStream spills r into a temporary file (EROFS needs io.ReaderAt),
// then unpacks it via goerofs.Unpack and removes the temp file.
func unpackEROFSStream(r io.Reader, dest string) (rerr error) {
	tmp, err := os.CreateTemp("", "unikraft-unpack-erofs-*.img")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { rerr = errors.Join(rerr, os.Remove(tmpPath)) }()

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return fmt.Errorf("reading rootfs: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing rootfs: %w", err)
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("reopening rootfs: %w", err)
	}
	defer f.Close()

	return goerofs.Unpack(f, dest)
}
