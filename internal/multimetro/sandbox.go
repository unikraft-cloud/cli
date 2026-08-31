// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"net/http"

	"unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"
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

	sb := SandboxClient{
		Client: c.sandbox.Client,
		Opts:   make([]sandbox.Option, 0, len(c.sandbox.Opts)+1),
	}
	sb.Opts = append(sb.Opts, c.sandbox.Opts...)
	if plugin != "" {
		sb.Opts = append(sb.Opts, sandbox.WithPluginName(plugin))
	}
	return sb
}

// SandboxInstance addresses the plugin endpoint of the instance a key refers
// to: a plugin client reaches its plugin through the instance's UUID alone.
func SandboxInstance(key Key) platform.Instance {
	return platform.Instance{Uuid: key.Ref().UUID}
}

// newSandboxClient builds a metro's plugin client from the same options its
// platform client is built from.
//
// sandbox.NewClient ignores the HTTP client those options carry and installs
// one of its own, so ours reaches the plugin per call in Opts instead. A call
// made without Opts silently uses the plugin's own client, which knows
// nothing of this metro's insecure setting.
func newSandboxClient(copts []platform.ClientOption, httpClient *http.Client) *SandboxClient {
	sb := &SandboxClient{Client: sandbox.NewClient(copts...)}
	if httpClient != nil {
		sb.Opts = append(sb.Opts, sandbox.WithHTTPClient(httpClient))
	}
	return sb
}
