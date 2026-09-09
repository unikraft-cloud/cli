// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package images

import (
	"context"

	dockerconfig "github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/session/auth/authprovider"

	"unikraft.com/cli/internal/config"
)

func LoadBuildkitAuthConfig(config *dockerconfig.ConfigFile, profile *config.Profile) authprovider.AuthConfigProvider {
	acp := &authConfigProvider{
		underlying: authprovider.LoadAuthConfig(config),
		profile:    profile,
	}
	return acp.load
}

type authConfigProvider struct {
	profile    *config.Profile
	underlying authprovider.AuthConfigProvider
}

func (ap *authConfigProvider) load(ctx context.Context, host string, scopes []string, cacheExpireCheck authprovider.ExpireCachedAuthCheck) (types.AuthConfig, error) {
	username, password, err := hostCreds(ap.profile, host)
	if err != nil {
		return types.AuthConfig{}, err
	}
	if username != "" || password != "" {
		return types.AuthConfig{
			Username: username,
			Password: password,
		}, nil
	}

	return ap.underlying(ctx, host, scopes, cacheExpireCheck)
}
