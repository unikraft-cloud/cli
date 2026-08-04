// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"context"
	"fmt"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/sandbox"
	"unikraft.com/x/iata"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
)

type MetroClient struct {
	platform.Client
	// Sandbox serves the sandbox plugin API of the same metro.  It cannot be
	// embedded alongside platform.Client, since both name their interface
	// Client.
	Sandbox sandbox.Client
	Metro   config.Metro
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
		client := platform.NewClient(
			platform.WithHTTPClient(httpclient.GetClient(ptr.ZeroIfNil(metro.Insecure))),
			platform.WithToken(profile.Token),
			platform.WithDefaultMetro(metro.Endpoint),
		)
		sandboxClient := sandbox.NewClient(
			sandbox.WithHTTPClient(httpclient.GetClient(ptr.ZeroIfNil(metro.Insecure))),
			sandbox.WithToken(profile.Token),
			sandbox.WithDefaultMetro(metro.Endpoint),
		)
		g = g.WithClient(
			metro.Name,
			MetroClient{Client: client, Sandbox: sandboxClient, Metro: metro},
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
