// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"net/http"

	"unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/version"
)

// SandboxClient is a sandbox plugin client for a single metro, bundled with
// the options every call through it has to carry. Only the sandbox commands
// need one; MetroClient hands it out through Sandbox.
type SandboxClient struct {
	Client *sandbox.Client
	Opts   []sandbox.Option
}

// Sandbox returns this metro's plugin client, targeting the named plugin. An
// empty name leaves the plugin the client was built with.
func (c MetroClient) Sandbox(plugin string) SandboxClient {
	if c.sandbox == nil {
		return SandboxClient{}
	}

	sb := SandboxClient{Client: c.sandbox.Client}
	sb.Opts = append(sb.Opts, c.sandbox.Opts...)
	if plugin != "" {
		sb.Opts = append(sb.Opts, sandbox.WithPluginName(plugin))
	}
	return sb
}

// newSandboxClient builds a metro's plugin client from the same options its
// platform client is built from, plus that client's own *http.Client.
func newSandboxClient(copts []platform.ClientOption, httpClient *http.Client) *SandboxClient {
	sb := &SandboxClient{Client: sandbox.NewClient(copts...)}
	sb.Opts = append(sb.Opts, sandbox.WithUserAgent(version.UserAgent()))
	if httpClient != nil {
		sb.Opts = append(sb.Opts, sandbox.WithHTTPClient(httpClient))
	}
	return sb
}
