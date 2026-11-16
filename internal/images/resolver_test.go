// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import (
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
