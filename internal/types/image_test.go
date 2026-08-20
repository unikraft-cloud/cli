// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

const testDigest = "sha256:43d3d758e6fba7d4734ac142cfdbf8aa786fcbbfd828017eecaadc5140a4b190"

func imageRef(t *testing.T, s string) types.ImageRef {
	t.Helper()
	var ref types.ImageRef
	require.NoError(t, ref.UnmarshalText([]byte(s)))
	return ref
}

func TestImageRef(t *testing.T) {
	tests := []struct {
		name string
		in   string
		// text is what MarshalText and long-form Render produce.
		text string
		// short is what short-form Render produces.
		short string
		// wire is what gets sent to the platform API.
		wire string
	}{
		{
			name:  "http+oci tag",
			in:    "http+oci://cdn.example.com/me/app/latest",
			text:  "http+oci://cdn.example.com/me/app/latest",
			short: "http+oci://cdn.example.com/me/app/latest",
			wire:  "http+oci://cdn.example.com/me/app/latest",
		},
		{
			name:  "https+oci digest is never shortened",
			in:    "https+oci://cdn.example.com/me/app/@" + testDigest,
			text:  "https+oci://cdn.example.com/me/app/@" + testDigest,
			short: "https+oci://cdn.example.com/me/app/@" + testDigest,
			wire:  "https+oci://cdn.example.com/me/app/@" + testDigest,
		},
		// Plain OCI references must keep behaving exactly as before.
		{
			name:  "bare reference",
			in:    "nginx",
			text:  "nginx",
			short: "nginx",
			wire:  "unikraft.io/official/nginx:latest",
		},
		{
			name:  "reference with digest",
			in:    "myuser/app@" + testDigest,
			text:  "myuser/app@" + testDigest,
			short: "myuser/app",
			wire:  "unikraft.io/myuser/app@" + testDigest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := imageRef(t, tt.in)

			text, err := ref.MarshalText()
			require.NoError(t, err)
			require.Equal(t, tt.text, string(text))

			long, err := ref.Render(value.RenderOpts{})
			require.NoError(t, err)
			require.Equal(t, tt.text, long)

			short, err := ref.Render(value.RenderOpts{Short: true})
			require.NoError(t, err)
			require.Equal(t, tt.short, short)

			require.Equal(t, tt.wire, ref.WireURL())

			// Marshalling and unmarshalling is a fixed point, which the patch
			// machinery relies on to avoid reporting spurious edits.
			require.Equal(t, ref, imageRef(t, tt.text))
		})
	}
}

func TestImageRefLink(t *testing.T) {
	kind, key, strong := imageRef(t, "http+oci://cdn.example.com/me/app/latest").Link()
	require.Equal(t, "image", kind)
	require.Equal(t, "http+oci://cdn.example.com/me/app/latest", key.String())
	require.False(t, strong)

	kind, key, strong = imageRef(t, "nginx").Link()
	require.Equal(t, "image", kind)
	require.Equal(t, "unikraft.io/official/nginx:latest", key.String())
	require.False(t, strong)
}

func TestImageRefZero(t *testing.T) {
	var ref types.ImageRef

	text, err := ref.MarshalText()
	require.NoError(t, err)
	require.Empty(t, text)

	rendered, err := ref.Render(value.RenderOpts{})
	require.NoError(t, err)
	require.Empty(t, rendered)

	require.Empty(t, ref.WireURL())

	_, _, ok := ref.Link()
	require.False(t, ok)
}

// ImageRef is compared against its zero value throughout and used as a lookup
// key, so it has to stay comparable by value.
var _ = map[types.ImageRef]struct{}{}

// Comparability is only useful if it is value equality.
func TestImageRefEquality(t *testing.T) {
	a, b := imageRef(t, "nginx:latest"), imageRef(t, "nginx:latest")

	// Compared with == deliberately, and via a variable so that testifylint
	// does not rewrite it.
	sameImage := a == b
	require.True(t, sameImage, "two references to the same image must be equal")

	seen := map[types.ImageRef]struct{}{a: {}}
	_, ok := seen[b]
	require.True(t, ok, "an equal reference must find its own map entry")

	differentImage := a == imageRef(t, "nginx:v1")
	require.False(t, differentImage, "references to different images must not be equal")
}
