// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"unikraft.com/x/image-spec/imageref"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
)

const testDigest = "sha256:43d3d758e6fba7d4734ac142cfdbf8aa786fcbbfd828017eecaadc5140a4b190"

// The grammar itself is pinned in unikraft.com/x/image-spec/imageref. What is
// the CLI's own is the policy it parses against.
func TestParseRef(t *testing.T) {
	for _, tt := range []struct {
		name   string
		key    string
		named  string
		scheme imageref.Scheme
		// wire is the identifier handed back to the platform.
		wire string
	}{
		{
			name:   "a bare reference gets the default registry and namespace",
			key:    "nginx",
			named:  "unikraft.io/official/nginx",
			scheme: imageref.SchemeOCI,
			wire:   "unikraft.io/official/nginx",
		},
		{
			name:   "the oci scheme is stripped on the wire",
			key:    "oci://index.unikraft.io/jedevc/http-oci-registry@" + testDigest,
			named:  "index.unikraft.io/jedevc/http-oci-registry@" + testDigest,
			scheme: imageref.SchemeOCI,
			wire:   "index.unikraft.io/jedevc/http-oci-registry@" + testDigest,
		},
		{
			// Docker Hub's namespace is "library/", not ours. Applying the
			// CLI's prefix here would name an image no registry can serve.
			name:   "docker hub keeps its own namespace",
			key:    "docker.io/nginx",
			named:  "docker.io/library/nginx",
			scheme: imageref.SchemeOCI,
			wire:   "docker.io/library/nginx",
		},
		{
			// The host comes from the URI, never from the CLI's default, and
			// the whole URI is what goes back to the platform.
			name:   "an http+oci uri keeps its own host",
			key:    "http+oci://dawn-sky-atgon69g.ukp-stable.apw.unikraft.internal/test/http-oci-registry/latest",
			scheme: imageref.SchemeHTTPOCI,
			wire:   "http+oci://dawn-sky-atgon69g.ukp-stable.apw.unikraft.internal/test/http-oci-registry/latest",
		},
		{
			name:   "an https+oci digest uri round-trips",
			key:    "https+oci://cdn.example.com/me/app/@" + testDigest,
			scheme: imageref.SchemeHTTPSOCI,
			wire:   "https+oci://cdn.example.com/me/app/@" + testDigest,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := images.ParseRef(tt.key)
			require.NoError(t, err)
			require.Equal(t, tt.scheme, parsed.Scheme())
			require.Equal(t, tt.wire, parsed.String())

			if tt.named == "" {
				// A layout served over HTTP has no registry name, and asking for
				// one has to fail rather than hand back a name that resolves to
				// an image on some other host.
				require.Nil(t, parsed.Named())
				_, err := images.ParseName(tt.key)
				require.ErrorContains(t, err, "has no registry name")
				return
			}

			require.Equal(t, tt.named, parsed.Named().String())

			// ParseName is the name-only view of the same parse.
			named, err := images.ParseName(tt.key)
			require.NoError(t, err)
			require.Equal(t, tt.named, named.String())
		})
	}
}

// An unusable identifier has to say what is actually wrong with it.
func TestParseRefErrors(t *testing.T) {
	for _, tt := range []struct{ name, key, wantErr string }{
		{"unknown scheme", "banana://x", `unsupported image URI scheme: "banana"`},
		{"a plain https url is not an image", "https://cdn.example.com/me/app", `unsupported image URI scheme: "https"`},
		{"a local path is not an image", "oci-archive:///tmp/image.tar", "addresses a local image"},
		{"a malformed uri says so once", "http+oci://cdn.example.com/me/app/", "unexpected terminating '/'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := images.ParseRef(tt.key)
			require.ErrorContains(t, err, tt.wantErr)
			require.NotContains(t, err.Error(), "invalid reference format: invalid reference format")
			require.NotContains(t, err.Error(), "invalid image reference: invalid image reference")
		})
	}
}

// A metro's index host supplies the default domain for a bare reference, but is
// then canonicalized onto the default registry.
func TestParseRefMetro(t *testing.T) {
	metro := &config.Metro{
		Name:     "stable",
		Endpoint: "https://api.stable.apw.unikraft.cloud",
	}
	require.Equal(t, "index.stable.apw.unikraft.cloud", metro.Index().Host)

	withMetro, err := images.ParseRefMetro(metro, "nginx")
	require.NoError(t, err)
	require.Equal(t, "unikraft.io/official/nginx", withMetro.Named().String())

	withoutMetro, err := images.ParseRef("nginx")
	require.NoError(t, err)
	require.Equal(t, withoutMetro, withMetro)

	// A host that is genuinely a different registry is left alone.
	other, err := images.ParseRefMetro(metro, "index.unikraft.io/me/app")
	require.NoError(t, err)
	require.Equal(t, "index.unikraft.io/me/app", other.Named().String())

	const uri = "http+oci://dawn-sky-atgon69g.ukp-stable.apw.unikraft.internal/test/http-oci-registry/latest"
	parsed, err := images.ParseRefMetro(metro, uri)
	require.NoError(t, err)
	require.Equal(t, "dawn-sky-atgon69g.ukp-stable.apw.unikraft.internal", parsed.Domain())
	require.Equal(t, "test/http-oci-registry", parsed.Path())
	require.Equal(t, uri, parsed.String())
}

// One policy drives both directions, so what parsing adds formatting takes back
// off again. A reference is only displayable in short form if it round-trips.
func TestPolicyFormatIsTheInverseOfParse(t *testing.T) {
	p := images.PolicyFor(nil)

	for _, key := range []string{
		"nginx",
		"nginx:v1",
		"myuser/app:v1",
		"official/utils/volimport:1.0",
		"index.unikraft.io/me/app:v1",
		"http+oci://cdn.example.com/me/app/latest",
	} {
		ref, err := p.Parse(key)
		require.NoError(t, err)

		formatted := p.Format(ref, false)
		back, err := p.Parse(formatted)
		require.NoError(t, err, "formatting %q gave %q", key, formatted)
		require.Equal(t, ref, back, "%q formatted to %q, which names a different image", key, formatted)
	}
}

// The policy has to be its own inverse for a metro too, not just for the default
// registry: parsing adds what an identifier leaves implicit and formatting takes
// exactly that back off.
func TestMetroPolicyFormatIsTheInverseOfParse(t *testing.T) {
	p := images.PolicyFor(&config.Metro{
		Name:     "fra0",
		Endpoint: "https://api.fra0.unikraft.cloud",
	})

	for _, key := range []string{"nginx", "nginx:v1", "myuser/app:v1", "index.fra0.unikraft.cloud/official/nginx"} {
		ref, err := p.Parse(key)
		require.NoError(t, err)

		formatted := p.Format(ref, false)
		back, err := p.Parse(formatted)
		require.NoError(t, err, "formatting %q gave %q", key, formatted)
		require.Equal(t, ref, back, "%q formatted to %q, which names a different image", key, formatted)
	}

	ref, err := p.Parse("index.fra0.unikraft.cloud/official/nginx")
	require.NoError(t, err)
	require.Equal(t, "nginx", p.Format(ref, false))

	// Another metro's index is not in scope, so it is left alone and shown.
	other, err := p.Parse("index.sfo0.unikraft.cloud/official/nginx")
	require.NoError(t, err)
	require.Equal(t, "index.sfo0.unikraft.cloud/official/nginx", p.Format(other, false))
}
