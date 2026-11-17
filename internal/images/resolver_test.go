// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import (
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
)

func TestHostCredsRegistryHost(t *testing.T) {
	profile := &config.Profile{
		Token:    base64.StdEncoding.EncodeToString([]byte("user:pass")),
		Registry: config.Registry{Host: "harbor.example:8083"},
	}

	username, password, err := hostCreds(profile, "harbor.example:8083")
	require.NoError(t, err)
	assert.Equal(t, "user", username)
	assert.Equal(t, "pass", password)
}

func TestResolverOptionsRegistry(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())

	t.Run("mirror", func(t *testing.T) {
		profile := &config.Profile{Registry: config.Registry{
			Host:   "harbor.example:8083",
			Mirror: "https://console.example.com/proxy/registry",
		}}

		hosts, err := resolverOptions(profile, nil, false).Hosts("harbor.example:8083")
		require.NoError(t, err)
		require.NotEmpty(t, hosts)
		assert.Equal(t, "console.example.com", hosts[0].Host)
	})

	t.Run("insecure", func(t *testing.T) {
		profile := &config.Profile{Registry: config.Registry{
			Host:     "harbor.example:8083",
			Insecure: true,
		}}
		options := resolverOptions(profile, nil, false)

		hosts, err := options.Hosts("harbor.example:8083")
		require.NoError(t, err)
		require.NotEmpty(t, hosts)
		assert.Same(t, httpclient.InsecureRegistryHTTPClient, hosts[0].Client)

		hosts, err = options.Hosts("ghcr.io")
		require.NoError(t, err)
		require.NotEmpty(t, hosts)
		assert.Same(t, httpclient.RegistryHTTPClient, hosts[0].Client)
	})
}

func TestInsecureTransport(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	insecure := &http.Client{Transport: insecureTransport(func(host string) (bool, error) {
		return host == u.Host, nil
	})}
	resp, err := insecure.Get(srv.URL)
	require.NoError(t, err)
	resp.Body.Close()

	secure := &http.Client{Transport: insecureTransport(func(string) (bool, error) {
		return false, nil
	})}
	_, err = secure.Get(srv.URL)
	var unknownAuthority x509.UnknownAuthorityError
	assert.ErrorAs(t, err, &unknownAuthority)
}
