// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"time"

	"github.com/containerd/platforms"
	"github.com/docker/go-units"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"unikraft.com/cloud/sdk/platform/group"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/builder"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/volimport"
)

func (c *VolumeImportCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	if c.Source == "" {
		return fmt.Errorf("source path is required")
	}

	// Resolve source to an absolute path.
	abs, err := filepath.Abs(c.Source)
	if err != nil {
		return fmt.Errorf("resolving source path: %w", err)
	}
	c.Source = abs

	if c.Port < 1024 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535")
	}

	// Resolve the target volume.
	gettable := partition.WrapGettable(Volume{})
	resources, err := gettable.Get(ctx, []string{c.Volume})
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		return fmt.Errorf("volume not found: %s", c.Volume)
	}
	if len(resources) > 1 {
		keys := make([]string, 0, len(resources))
		for _, res := range resources {
			keys = append(keys, res.Key().String())
		}
		return fmt.Errorf("ambiguous volume: %s (found %v)", c.Volume, keys)
	}
	vol, ok := resources[0].(Volume)
	if !ok {
		return fmt.Errorf("unexpected resource type %T", resources[0])
	}

	sourceType, err := builder.DetectSourceType(c.Source)
	if err != nil {
		return fmt.Errorf("detecting source type for %q: %w", c.Source, err)
	}
	plat := platforms.DefaultSpec()
	plat.OS = imagespec.PlatformKraftcloud
	importOpts := &builder.BuildOpts{
		Rootfs: builder.FSOpts{
			Path: c.Source,
			Type: sourceType,
			// Set format to CPIO as volimport expects a CPIO archive
			Format: kraftfile.FsTypeCpio,
		},
		Platform: []ocispec.Platform{plat},
	}

	// Build an import archive from the data source.
	log.G(ctx).Trace().Str("source", c.Source).Msg("packaging source as import archive")
	images, err := builder.BuildRootfs(ctx, *importOpts)
	if err != nil {
		return fmt.Errorf("packaging source as import archive: %w", err)
	}
	if len(images) == 0 {
		return fmt.Errorf("no images were built from the provided source")
	}
	if len(images) > 1 {
		return fmt.Errorf("multiple images were built from the provided source; expected exactly one")
	}
	initrd := images[0].Initrd
	if initrd == nil {
		return fmt.Errorf("built image has no initrd")
	}
	defer func() {
		if err := initrd.Cleanup(); err != nil {
			log.G(ctx).Error().Err(err).Msg("cleaning up initrd")
		}
	}()

	cpioReader, cpioSize, err := initrd.Open(ctx)
	if err != nil {
		return fmt.Errorf("opening import archive: %w", err)
	}
	defer cpioReader.Close()

	authStr, err := volimport.GenRandAuth()
	if err != nil {
		return fmt.Errorf("generating authentication token: %w", err)
	}

	// Spawn a temporary volimport instance in the volume's metro.
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	var instUUID, instFQDN string
	var metroInsecure bool
	if err := group.DoMetro(ctx, g, string(vol.Metro), func(ctx context.Context, mc multimetro.MetroClient) error {
		metroInsecure = ptr.ZeroIfNil(mc.Metro.Insecure)
		var merr error
		volimportTimeout := uint64(10)
		if deadline, ok := ctx.Deadline(); ok {
			t := max(
				// don't run past deadline (+2s for tolerance)
				time.Until(deadline)+2*time.Second,
				// set reasonable max timeout
				10*time.Second,
			)
			volimportTimeout = uint64(math.Ceil(t.Seconds()))
		}
		instUUID, instFQDN, merr = volimport.Start(ctx, mc, c.Image, vol.UUID, authStr, volimportTimeout, c.Port)
		return merr
	}); err != nil {
		return fmt.Errorf("spawning volume data import instance: %w", err)
	}

	defer func() {
		if err := group.DoMetro(ctx, g, string(vol.Metro), func(ctx context.Context, mc multimetro.MetroClient) error {
			return volimport.Terminate(ctx, mc, instUUID)
		}); err != nil {
			log.G(ctx).Error().Err(err).Msg("terminating volume data import instance")
		}
	}()

	// Open a TLS connection to the instance and stream the CPIO archive.
	instAddr := instFQDN + ":" + strconv.Itoa(c.Port)
	log.G(ctx).Info().
		Str("size", units.BytesSize(float64(cpioSize))).
		Str("volume", c.Volume).
		Msg("importing data into volume")

	conn, err := tls.Dial("tcp", instAddr, &tls.Config{
		InsecureSkipVerify: metroInsecure,
	})
	if err != nil {
		return fmt.Errorf("connecting to volume import service at %s: %w", instAddr, err)
	}
	defer conn.Close()

	freeSpace, totalSpace, err := volimport.Copy(ctx, conn, authStr, cpioReader, c.Force, uint64(cpioSize))
	if err != nil {
		return fmt.Errorf("importing data: %w", err)
	}

	log.G(ctx).Info().
		Str("volume", c.Volume).
		Str("free", units.BytesSize(float64(freeSpace))).
		Str("total", units.BytesSize(float64(totalSpace))).
		Msg("import complete")

	// Wait for the import instance to stop; it auto-deletes via delete-on-stop.
	return group.DoMetro(ctx, g, string(vol.Metro), func(ctx context.Context, mc multimetro.MetroClient) error {
		return volimport.Wait(ctx, mc, instUUID)
	})
}
