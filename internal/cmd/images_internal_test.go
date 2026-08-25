// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"unikraft.com/cloud/sdk/controlplane"
	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

func imageEntry(t *testing.T, key string) ImageEntry {
	t.Helper()
	var ref types.ImageRef
	require.NoError(t, ref.UnmarshalText([]byte(key)))
	return ImageEntry{Ref: ref}
}

func imageLookupKey(t *testing.T, key string) imageKey {
	t.Helper()
	var ref types.ImageRef
	require.NoError(t, ref.UnmarshalText([]byte(key)))
	return imageKey{ref: ref.Reference()}
}

// Two URIs differing only in transport or host decompose to the same reference,
// so matching on the reference would return an image served from somewhere else.
func TestImageKeyMatches(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		entry string
		want  bool
	}{
		{
			name:  "same URI",
			key:   "http+oci://cdn.example.com/me/app/latest",
			entry: "http+oci://cdn.example.com/me/app/latest",
			want:  true,
		},
		{
			name:  "transport differs",
			key:   "http+oci://cdn.example.com/me/app/latest",
			entry: "https+oci://cdn.example.com/me/app/latest",
			want:  false,
		},
		{
			name:  "host differs",
			key:   "http+oci://cdn.example.com/me/app/latest",
			entry: "http+oci://other.example.com/me/app/latest",
			want:  false,
		},
		{
			// The reference a URI decomposes to does not address it.
			name:  "reference does not match a URI entry",
			key:   "cdn.example.com/me/app:latest",
			entry: "http+oci://cdn.example.com/me/app/latest",
			want:  false,
		},
		{
			name:  "URI does not match a registry entry",
			key:   "http+oci://cdn.example.com/me/app/latest",
			entry: "cdn.example.com/me/app:latest",
			want:  false,
		},
		// Registry references keep matching as before, including the familiar
		// short forms.
		{
			name:  "registry reference",
			key:   "unikraft.io/official/nginx:latest",
			entry: "unikraft.io/official/nginx:latest",
			want:  true,
		},
		{
			name:  "familiar short form",
			key:   "nginx",
			entry: "unikraft.io/official/nginx:latest",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := imageLookupKey(t, tt.key)
			require.Equal(t, tt.want, key.matches(imageEntry(t, tt.entry)))
		})
	}
}

// A key reports itself the way the caller addressed it, so a lookup that finds
// nothing names the URI rather than the reference it decomposes to.
func TestImageKeyString(t *testing.T) {
	require.Equal(t,
		"http+oci://cdn.example.com/me/app/latest",
		imageLookupKey(t, "http+oci://cdn.example.com/me/app/latest").String())
	require.Equal(t,
		"unikraft.io/official/nginx:latest",
		imageLookupKey(t, "nginx").String())
}

// A controlplane image the image store cannot address as a registry reference
// must not abort the listing.
func TestLoadFromControlplaneDoesNotFailOnHTTPImage(t *testing.T) {
	const uri = "http+oci://cdn.example.com/me/app/latest"

	entries, err := ImageEntry{}.loadFromControlplane(controlplane.Image{
		Name: uri,
		Tags: []controlplane.ImageTag{{Name: "latest"}},
	})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// It is addressed by URI, not by the name it decomposes to: that name says
	// nothing about which host serves it.
	require.Equal(t, uri, entries[0].Ref.WireURL())
}

// Whatever a listing displays has to be something the sibling get command can
// look up.
func TestLoadFromPlatformNamesImagesAddressably(t *testing.T) {
	metro := config.Metro{Name: "fra0", Endpoint: "https://api.fra0.unikraft.cloud"}

	for _, tt := range []struct{ url, short, wire string }{
		// A registry that is genuinely somewhere else keeps its name.
		{"myregistry.io/nginx", "myregistry.io/nginx", "myregistry.io/nginx:latest"},
		{"docker.io/library/nginx", "docker.io/library/nginx", "docker.io/library/nginx:latest"},

		// The metro's own index is the default registry under another name, so
		// it is canonicalized and shows in the short form a human types.
		{"index.fra0.unikraft.cloud/official/nginx", "nginx", "unikraft.io/official/nginx:latest"},
		{"nginx", "nginx", "unikraft.io/official/nginx:latest"},

		// A layout served over HTTP is addressed by its URI throughout.
		{
			"http+oci://cdn.example.com/me/app/latest",
			"http+oci://cdn.example.com/me/app/latest",
			"http+oci://cdn.example.com/me/app/latest",
		},
	} {
		t.Run(tt.url, func(t *testing.T) {
			entries, err := ImageEntry{}.loadFromPlatform(
				platform.Image{Url: tt.url, Tags: []string{"latest"}}, &metro)
			require.NoError(t, err)
			require.NotEmpty(t, entries)
			entry := entries[0]

			short, err := entry.Ref.Render(value.RenderOpts{Short: true})
			require.NoError(t, err)
			require.Equal(t, tt.short, short)
			require.Equal(t, tt.wire, entry.Ref.WireURL())

			// The displayed name has to address the image it was displayed for.
			ref, err := images.ParseRef(short)
			require.NoError(t, err, "the displayed name %q does not even parse", short)
			require.True(t, imageKey{ref: ref}.matches(entry),
				"listing shows %q, which image get cannot find", short)
		})
	}
}
