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
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
)

type MetroClient struct {
	platform.Client
	Metro config.Metro
}

func NewClient(ctx context.Context) (*group.Group[MetroClient], error) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	if len(profile.Metros) == 0 {
		return nil, fmt.Errorf("profile %q has no metros configured", profile.Name)
	}
	metros := profile.Metros
	metros = filterMetrosFromContext(ctx, metros)

	metroNames := make([]string, 0, len(metros))
	for _, metro := range metros {
		metroNames = append(metroNames, metro.Name.String())
	}
	log.G(ctx).
		Trace().
		Strs("metros", metroNames).
		Msg("initializing platform clients")

	group := group.New[MetroClient]()
	for _, metro := range metros {
		client := platform.NewClient(
			platform.WithHTTPClient(httpclient.GetClient(metro.Insecure)),
			platform.WithToken(profile.Token.String()),
			platform.WithDefaultMetro(metro.Endpoint.String()),
		)
		group = group.WithClient(
			metro.Name.String(),
			MetroClient{Client: client, Metro: metro},
		)
	}
	return group, nil
}
