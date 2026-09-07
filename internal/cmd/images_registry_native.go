// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/joinerrgroup"
	"unikraft.com/x/kingkong"

	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
)

type Image struct {
	Ref    types.ImageRef[reference.Named] `field:",short"`
	Digest digest.Digest                   `field:",long"`

	Config   ImageConfig   `field:",embed"`
	Metadata ImageMetadata `field:",long,embed"`

	Kernel      *ImageFile  `field:",long,embed"`
	KernelDebug *ImageFile  `field:"kernel.dbg,long,embed"`
	Initrd      *ImageFile  `field:",long,embed"`
	Roms        []ImageFile `field:",long,embed"`

	Image imagespec.Image `field:"-" json:"image"`
}

func (Image) Type() resource.Type {
	return resource.Type{
		Name:  "image",
		Names: "images",
	}
}

func (i Image) Key() resource.Key {
	return staticKey(i.Ref.Reference.String())
}

func (i Image) Raw() any {
	return i.Image.Descriptor
}

func (i Image) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

func imageFileFrom(file imagespec.File) *ImageFile {
	if file == nil {
		return nil
	}
	desc, _ := file.Source()
	return &ImageFile{
		Digest:      desc.Digest,
		MediaType:   desc.MediaType,
		Annotations: desc.Annotations,
		Size:        types.SizeBytes(desc.Size),
	}
}

func imageRomsFrom(files []imagespec.File) []ImageFile {
	if len(files) == 0 {
		return nil
	}
	roms := make([]ImageFile, 0, len(files))
	for _, file := range files {
		rom := imageFileFrom(file)
		if rom == nil {
			continue
		}
		roms = append(roms, *rom)
	}
	if len(roms) == 0 {
		return nil
	}
	return roms
}

func (Image) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	access, err := images.Accessor(ctx)
	if err != nil {
		return nil, err
	}

	perKey := make([][]resource.Resource, len(keys))
	eg := joinerrgroup.Group{}
	for i, key := range keys {
		eg.Go(func() error {
			src, err := imagespec.GuessURI(key)
			if err != nil {
				return fmt.Errorf("parsing image reference %q: %w", key, err)
			}
			imgs, err := access.LoadAll(ctx, src, platforms.All)
			if err != nil {
				if errdefs.IsNotFound(err) {
					return group.ErrRefNotFound{Refs: group.Refs{{Name: key}}}
				}
				return fmt.Errorf("failed to resolve image %q: %w", key, err)
			}
			defer func() {
				for _, img := range imgs {
					img.Close()
				}
			}()

			for _, img := range imgs {
				config := img.Image
				envs := make(map[string]string, len(config.Config.Env))
				for _, entry := range config.Config.Env {
					if key, val, ok := strings.Cut(entry, "="); ok {
						envs[key] = val
					}
				}

				meta := img.Metadata()
				resource := Image{
					Ref: types.ImageRef[reference.Named]{
						Reference: img.Name,
					},
					Digest: img.Descriptor.Digest,
					Config: ImageConfig{
						Cmd:      config.Config.Cmd,
						Env:      envs,
						Platform: types.Platform(config.Platform),
					},
					Metadata: ImageMetadata{
						Author:  meta.Author,
						Created: meta.Created,
					},
					Kernel:      imageFileFrom(img.Kernel),
					KernelDebug: imageFileFrom(img.KernelDebug),
					Initrd:      imageFileFrom(img.Initrd),
					Roms:        imageRomsFrom(img.Roms),
					Image:       *img,
				}
				perKey[i] = append(perKey[i], &resource)
			}
			return nil
		})
	}
	err = eg.Wait()
	return slices.Concat(perKey...), err
}

func (Image) Delete(ctx context.Context, keys []string) error {
	access, err := images.Accessor(ctx)
	if err != nil {
		return err
	}

	eg := joinerrgroup.Group{}
	for _, key := range keys {
		eg.Go(func() error {
			uri, err := imagespec.GuessURI(key)
			if err != nil {
				return fmt.Errorf("parsing image reference %q: %w", key, err)
			}
			if err := access.Delete(ctx, uri); err != nil {
				if errdefs.IsNotFound(err) {
					return group.ErrRefNotFound{Refs: group.Refs{{Name: key}}}
				}
				return fmt.Errorf("deleting image %q: %w", key, err)
			}

			return nil
		})
	}
	return eg.Wait()
}

func (Image) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect an image by tag",
				Commands:    []string{"unikraft image get nginx:latest"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all images",
				Commands:    []string{"unikraft image list"},
			},
			{
				Description: "Filter images by reference",
				Commands:    []string{`unikraft image list --filter 'ref~="/nginx"'`},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a remote image",
				Commands:    []string{"unikraft image delete unikraft.io/official/nginx:latest"},
			},
		},
	}
}

func (cmd ImagesCopyCmd) Run(ctx context.Context, partition *resource.Partition) error {
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
	dest, err := imagespec.GuessURI(cmd.Dest)
	if err != nil {
		return fmt.Errorf("parsing destination image reference: %w", err)
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

	err = access.Save(ctx, dest, imgs...)
	if err != nil {
		return fmt.Errorf("saving image to destination: %w", err)
	}

	if partition != nil && dest.Scheme == imagespec.URISchemeOCI {
		if err := addImageToPartition(ctx, partition, dest.Path); err != nil {
			return fmt.Errorf("adding copied image to partition: %w", err)
		}
	}

	return nil
}
