// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package httpclient

import (
	"net/http"
	"slices"
	"time"

	sdkhttpclient "unikraft.com/cloud/sdk/pkg/httpclient"
	"unikraft.com/x/version"
)

// GetClient returns an HTTP client based on the provided insecure flag.
func GetClient(insecure bool) *http.Client {
	if insecure {
		return InsecureHTTPClient
	}
	return DefaultHTTPClient
}

// Platform calls can wait 60 s for an operation. All clients wait up to 3
// minutes for response headers.
var options = []sdkhttpclient.Option{
	sdkhttpclient.WithUserAgent(version.UserAgent()),
	sdkhttpclient.WithResponseHeaderTimeout(3 * time.Minute),
}

// DefaultHTTPClient is the default HTTP client used by the Unikraft CLI. It
// uses the environment's proxy settings and sets the CLI's User-Agent on
// requests that don't already set one.
var DefaultHTTPClient = sdkhttpclient.NewHTTPClient(options...)

// InsecureHTTPClient is an HTTP client that skips TLS verification. It is
// intended for use in development or testing environments where self-signed
// certificates may be used.
var InsecureHTTPClient = sdkhttpclient.NewHTTPClient(
	append(slices.Clone(options), sdkhttpclient.WithInsecure())...,
)

// RegistryHTTPClient is the HTTP client used for container registry traffic.
// It waits up to 3 minutes for response headers.
var RegistryHTTPClient = sdkhttpclient.NewHTTPClient(options...)

// InsecureRegistryHTTPClient is RegistryHTTPClient with TLS verification
// skipped.
var InsecureRegistryHTTPClient = sdkhttpclient.NewHTTPClient(
	append(slices.Clone(options), sdkhttpclient.WithInsecure())...,
)
