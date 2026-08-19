// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"context"
	"fmt"
	"net/http"

	"unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/iata"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
)

type MetroClient struct {
	platform.Client
	Sandbox *sandbox.Client
	Metro   config.Metro

	// sandboxHTTPClient is the HTTP client sandbox plugin calls on this metro
	// are made with. The plugin client is built from the platform client's
	// options, which carry no HTTP client of their own, so it is handed to the
	// plugin per call instead.
	sandboxHTTPClient *http.Client
}

// SandboxOpts returns the options for a call against the named plugin on this
// metro. An empty plugin name leaves the plugin the client was built with.
func (c MetroClient) SandboxOpts(plugin string) []sandbox.Option {
	opts := make([]sandbox.Option, 0, 2)
	if c.sandboxHTTPClient != nil {
		opts = append(opts, sandbox.WithHTTPClient(c.sandboxHTTPClient))
	}
	if plugin != "" {
		opts = append(opts, sandbox.WithPluginName(plugin))
	}
	return opts
}

// SandboxInstance addresses the plugin endpoint of the instance a key refers
// to: a plugin client reaches its plugin through the instance's UUID alone.
func SandboxInstance(key Key) platform.Instance {
	return platform.Instance{Uuid: key.Ref().UUID}
}

func NewClient(ctx context.Context) (*group.Group[MetroClient], error) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}

	metros := profile.Metros
	if len(profile.Metros) == 0 {
		if profile.ControlPlane == "" {
			return nil, fmt.Errorf("profile %q has no metros configured", profile.Name)
		}
		metros, err = GetMetros(ctx, profile)
		if err != nil {
			return nil, err
		}
	}
	g := group.New[MetroClient]()
	for _, metro := range metros {
		httpClient := httpclient.GetClient(ptr.ZeroIfNil(metro.Insecure))
		copts := []platform.ClientOption{
			platform.WithHTTPClient(httpClient),
			platform.WithToken(profile.Token),
			platform.WithDefaultMetro(metro.Endpoint),
		}
		g = g.WithClient(
			metro.Name,
			MetroClient{
				Client:            platform.NewClient(copts...),
				Sandbox:           sandbox.NewClient(copts...),
				Metro:             metro,
				sandboxHTTPClient: httpClient,
			},
		)
	}

	g = g.Filter(filterMetrosFromCtx(ctx, g.Names()))
	log.G(ctx).
		Trace().
		Strs("metros", g.Names()).
		Msg("initializing platform clients")

	return g, nil
}

func GetMetros(ctx context.Context, profile *config.Profile) ([]config.Metro, error) {
	log.G(ctx).Trace().
		Str("controlplane", profile.ControlPlane).
		Msg("fetching metros")

	client, err := NewControlClientFromProfile(profile)
	if err != nil {
		return nil, err
	}

	metroResp, err := client.ListMetros(ctx)
	if err != nil {
		return nil, err
	}
	if metroResp == nil || metroResp.Data == nil {
		return nil, nil
	}

	var metros []config.Metro
	for _, metro := range metroResp.Data.Metros {
		// Normalize the metro's IATA code via the iata package.
		location := metro.IataCode
		if matched := iata.ToIata(location); matched != iata.IataUnknown {
			location = matched.Code
		}
		metros = append(metros, config.Metro{
			Name:     metro.Name,
			Endpoint: metro.Endpoint,
			Location: location,
		})
	}
	return metros, nil
}
