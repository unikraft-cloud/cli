// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import (
	"context"

	"github.com/distribution/reference"
	"github.com/opencontainers/go-digest"
	"unikraft.com/x/kingkong"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
)

// Image identifies a registry image.  The web console has no local image
// store, so only the fields which name an image are kept.
type Image struct {
	Ref    types.ImageRef[reference.Named] `field:",short"`
	Digest digest.Digest                   `field:",long"`
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
	return i.Ref.Reference.String()
}

func (i Image) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

// Get reports that inspecting an image needs a registry client.
func (Image) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	return nil, notAvailable("image inspect")
}

// Delete reports that deleting an image needs a registry client.
func (Image) Delete(ctx context.Context, keys []string) error {
	return notAvailable("image delete")
}

func (Image) Examples() map[cmd.CmdType][]kingkong.Example {
	return nil
}

// Run reports that copying images needs a registry client.
func (cmd ImagesCopyCmd) Run(ctx context.Context, partition *resource.Partition) error {
	return notAvailable("image copy")
}
