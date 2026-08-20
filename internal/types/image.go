// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import (
	ociref "github.com/distribution/reference"
	"unikraft.com/x/image-spec/imageref"

	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/value"
)

// ImageRef is a resource field holding an image reference, which is either an
// image in a registry or an OCI layout served over HTTP. The distinction is the
// parsed reference's to make.
type ImageRef struct {
	ref imageref.Reference
}

// NewImageRef returns an ImageRef for an image that is already named, which is
// always an image in a registry. A nil name yields the unset reference.
func NewImageRef(named ociref.Named) ImageRef {
	ref, err := imageref.FromNamed(named)
	if err != nil {
		return ImageRef{}
	}
	return ImageRef{ref: ref}
}

// NewImageRefFrom returns an ImageRef for an already-parsed reference.
func NewImageRefFrom(ref imageref.Reference) ImageRef {
	return ImageRef{ref: ref}
}

// Reference returns the parsed reference, which is the zero Reference when the
// field is unset.
func (ir ImageRef) Reference() imageref.Reference {
	return ir.ref
}

func (ir ImageRef) MarshalText() ([]byte, error) {
	return []byte(images.Format(ir.ref)), nil
}

// Render implements value.Renderer. In short form (e.g. table output) the digest
// is elided to keep output concise.
func (ir ImageRef) Render(opts value.RenderOpts) (string, error) {
	if opts.Short {
		return images.FormatShort(ir.ref), nil
	}
	return images.Format(ir.ref), nil
}

func (ir ImageRef) Value() any {
	return ir
}

func (ir *ImageRef) UnmarshalText(text []byte) error {
	ref, err := images.ParseRef(string(text))
	if err != nil {
		return err
	}
	ir.ref = ref.WithDefaultTag()
	return nil
}

// WireURL returns the identifier to send to the platform API.
// It differs from MarshalText, which renders the familiar short form a human
// reads: the API resolves what it is given against its own registry and applies
// no namespace of its own, so "nginx:latest" there is not the image
// "unikraft.io/official/nginx:latest" that the CLI means by it.
func (ir ImageRef) WireURL() string {
	return ir.ref.String()
}

func (ir ImageRef) Link() (string, resource.Key, bool) {
	if ir.ref.IsZero() {
		return "", nil, false
	}
	return "image", multimetro.Key{
		Name: ir.WireURL(),
	}, false
}
