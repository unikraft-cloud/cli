// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/model/patch"
	"unikraft.com/cli/internal/types"
)

const (
	imageCacheDigestA = "sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"
	imageCacheDigestB = "sha256:b3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"
)

// TestImageCacheLoad checks the rows that one platform image gives.
func TestImageCacheLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		image platform.Image
		keys  []string
	}{
		{
			name:  "other registry keeps its host",
			image: platform.Image{Url: "oci://docker.io/library/nginx@" + imageCacheDigestA, Tags: []string{"1.27"}},
			keys:  []string{"docker.io/library/nginx:1.27@" + imageCacheDigestA},
		},
		{
			name:  "one row for each tag",
			image: platform.Image{Url: "oci://unikraft.io/myuser/app@" + imageCacheDigestA, Tags: []string{"v1", "latest"}},
			keys: []string{
				"unikraft.io/myuser/app:v1@" + imageCacheDigestA,
				"unikraft.io/myuser/app:latest@" + imageCacheDigestA,
			},
		},
		{
			name: "no reference",
			image: platform.Image{
				Uuid: new("7b79b250-0658-4d10-8dc0-d854399d7e74"),
				Url:  "7b79b250-0658-4d10-8dc0-d854399d7e74",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows, err := ImageCache{}.load(tt.image, "fra")
			require.NoError(t, err)

			var keys []string
			for _, row := range rows {
				keys = append(keys, row.Key().String())
			}
			assert.Equal(t, tt.keys, keys)
		})
	}
}

// TestMergeImageCache checks that each image gets the metros of all its rows.
func TestMergeImageCache(t *testing.T) {
	t.Parallel()

	type cached struct {
		metro string
		image platform.Image
	}
	type row struct {
		key      string
		active   []string
		pinned   []string
		autokill map[string]types.DurationMS
	}
	app := func(digest string, tags ...string) platform.Image {
		return platform.Image{Url: "oci://unikraft.io/myuser/app@" + digest, Tags: tags}
	}
	pinned := app(imageCacheDigestA, "v1")
	pinned.Persistent = new(true)
	pinned.Autokill = &platform.ImageAutokill{TimeMs: 3600000}

	tests := []struct {
		name   string
		cached []cached
		rows   []row
	}{
		{
			name: "one image on two metros",
			cached: []cached{
				{"fra", app(imageCacheDigestA, "v1")},
				{"sfo", pinned},
			},
			rows: []row{{
				key:      "unikraft.io/myuser/app:v1@" + imageCacheDigestA,
				active:   []string{"fra", "sfo"},
				pinned:   []string{"sfo"},
				autokill: map[string]types.DurationMS{"sfo": 3600000},
			}},
		},
		{
			name: "tag on one metro only",
			cached: []cached{
				{"sfo", app(imageCacheDigestA)},
				{"fra", app(imageCacheDigestA, "v1")},
			},
			rows: []row{{
				key:    "unikraft.io/myuser/app:v1@" + imageCacheDigestA,
				active: []string{"fra", "sfo"},
			}},
		},
		{
			name: "one tag on two images",
			cached: []cached{
				{"fra", app(imageCacheDigestA, "latest")},
				{"fra", app(imageCacheDigestB, "latest")},
			},
			rows: []row{
				{key: "unikraft.io/myuser/app:latest@" + imageCacheDigestA, active: []string{"fra"}},
				{key: "unikraft.io/myuser/app:latest@" + imageCacheDigestB, active: []string{"fra"}},
			},
		},
		{
			name:   "no tag on any metro",
			cached: []cached{{"fra", app(imageCacheDigestA)}, {"sfo", app(imageCacheDigestA)}},
			rows: []row{{
				key:    "unikraft.io/myuser/app@" + imageCacheDigestA,
				active: []string{"fra", "sfo"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var loaded []ImageCache
			for _, c := range tt.cached {
				rows, err := ImageCache{}.load(c.image, c.metro)
				require.NoError(t, err)
				loaded = append(loaded, rows...)
			}

			var rows []row
			for _, merged := range mergeImageCache(loaded) {
				rows = append(rows, row{
					key:      merged.Key().String(),
					active:   merged.Active,
					pinned:   merged.Pinned,
					autokill: merged.Autokill,
				})
			}
			assert.Equal(t, tt.rows, rows)
		})
	}
}

// TestImageCacheRequests checks the requests that an edit sends to each metro.
func TestImageCacheRequests(t *testing.T) {
	t.Parallel()

	// The image is in the cache of fra only. It is pinned there for one hour.
	image := ImageCache{
		Digest:   imageCacheDigestA,
		Active:   []string{"fra"},
		Pinned:   []string{"fra"},
		Autokill: map[string]types.DurationMS{"fra": 3600000},
		Images: map[string]platform.Image{
			"fra": {
				Uuid: new("7b79b250-0658-4d10-8dc0-d854399d7e74"),
				Url:  "oci://unikraft.io/myuser/app@" + imageCacheDigestA,
			},
		},
	}
	require.NoError(t, image.Ref.UnmarshalText([]byte("myuser/app:v1")))

	const (
		byTag    = "unikraft.io/myuser/app:v1"
		byDigest = "unikraft.io/myuser/app@" + imageCacheDigestA
	)
	autokill := func(ms uint64) *platform.PinImageRequestItemAutokill {
		return &platform.PinImageRequestItemAutokill{TimeMs: ms}
	}

	tests := []struct {
		name   string
		spec   patch.PatchSpec
		pins   map[string]platform.PinImageRequestItem
		unpins map[string]platform.UnpinImageRequestItem
		err    string
	}{
		{
			name: "pin with autokill",
			spec: patch.PatchSpec{Add: map[string][]string{"pinned": {"sfo"}, "autokill": {"sfo=24h"}}},
			pins: map[string]platform.PinImageRequestItem{"sfo": {Url: byTag, TimeoutS: -1, Autokill: autokill(86400000)}},
		},
		{
			name: "pin again keeps the autokill",
			spec: patch.PatchSpec{Add: map[string][]string{"pinned": {"fra"}}},
			pins: map[string]platform.PinImageRequestItem{"fra": {Url: byDigest, TimeoutS: -1, Autokill: autokill(3600000)}},
		},
		{
			name: "change the autokill",
			spec: patch.PatchSpec{Add: map[string][]string{"autokill": {"fra=2h"}}},
			pins: map[string]platform.PinImageRequestItem{"fra": {Url: byDigest, TimeoutS: -1, Autokill: autokill(7200000)}},
		},
		{
			name: "remove the autokill",
			spec: patch.PatchSpec{Del: map[string][]string{"autokill": {"fra"}}},
			pins: map[string]platform.PinImageRequestItem{"fra": {Url: byDigest, TimeoutS: -1}},
		},
		{
			name: "unpin",
			spec: patch.PatchSpec{Del: map[string][]string{"pinned": {"fra"}}},
			unpins: map[string]platform.UnpinImageRequestItem{
				"fra": {Uuid: new("7b79b250-0658-4d10-8dc0-d854399d7e74")},
			},
		},
		{
			name: "autokill without a pin",
			spec: patch.PatchSpec{Add: map[string][]string{"autokill": {"sfo=1h"}}},
			err:  `autokill on metro "sfo" needs the image pinned there`,
		},
		{
			name: "autokill of zero",
			spec: patch.PatchSpec{Add: map[string][]string{"autokill": {"fra=0"}}},
			err:  "autokill must be greater than 0, use --del autokill=fra to remove it",
		},
		{
			name: "unpin on a metro without the image",
			spec: patch.PatchSpec{Del: map[string][]string{"pinned": {"sfo"}}},
			err:  `image is not cached on metro "sfo"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fields, err := image.Fields(t.Context())
			require.NoError(t, err)
			patched, err := patch.PatchedFields(t.Context(), fields, tt.spec)
			require.NoError(t, err)

			pins, unpins, err := image.requests(patched)
			if tt.err != "" {
				require.EqualError(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, pins, len(tt.pins))
			assert.Len(t, unpins, len(tt.unpins))
			for metro, want := range tt.pins {
				assert.Equal(t, want, pins[metro])
			}
			for metro, want := range tt.unpins {
				assert.Equal(t, want, unpins[metro])
			}
		})
	}
}
