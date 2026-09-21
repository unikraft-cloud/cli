// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package httpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkhttpclient "unikraft.com/cloud/sdk/pkg/httpclient"
)

// TestRegistryClientHasNoRequestTimeout pins the regression that broke pushing
// a multi-gigabyte rootfs: http.Client.Timeout covers writing the request body,
// so any non-zero value kills a large blob PUT mid-upload.
func TestRegistryClientHasNoRequestTimeout(t *testing.T) {
	assert.Zero(t, RegistryHTTPClient.Timeout)
	assert.Zero(t, InsecureRegistryHTTPClient.Timeout)
}

// TestRegistryTransportDefaults pins that the registry transport waits forever
// for response headers, which a registry needs while it commits a large blob,
// and that supplying our own transport did not drop the connection pool tuning
// the SDK applies to its default one.
func TestRegistryTransportDefaults(t *testing.T) {
	transport := newRegistryTransport()

	assert.Zero(t, transport.ResponseHeaderTimeout)
	assert.Equal(t, 500, transport.MaxIdleConns)
	assert.Equal(t, 100, transport.MaxIdleConnsPerHost)
	assert.NotNil(t, transport.DialContext)
}

// TestRegistryTransportReachesClient pins the SDK behaviour the registry
// clients rely on: a transport passed with WithTransport is the one that
// serves requests.
func TestRegistryTransportReachesClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 20 * time.Millisecond

	_, err := sdkhttpclient.NewHTTPClient(
		sdkhttpclient.WithUserAgent("test"),
		sdkhttpclient.WithTransport(transport),
	).Get(server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout awaiting response headers")
}
