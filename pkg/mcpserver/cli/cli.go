// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package cli serves the MCP server of unikraft mcp to other CLIs.
package cli

import (
	"context"
	"errors"
	"os"

	"unikraft.com/cloud/sdk/controlplane"

	"unikraft.com/cli/internal/branding"
	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/pkg/mcpserver"
)

// New returns the server of unikraft mcp, with every tool and the
// documentation as resources. Its tool calls use the profile that WithToken
// puts on their context. It serves HTTPS with the TLS files of flags.
func New(ctx context.Context, flags mcpserver.Flags) (*mcpserver.Server, error) {
	if err := flags.Check(); err != nil {
		return nil, err
	}
	// A pinned profile drops profile_use, which writes the configuration file.
	ctx = WithToken(ctx, "")
	// The profile of the environment wins over the one of WithToken.
	if config.G(ctx).CurrentProfileName() == config.EnvProfile {
		return nil, errors.New("UNIKRAFT_PROFILE_ENV is set, so every tool call would use UKC_TOKEN and not its own token")
	}
	return cmd.NewMCPServer(ctx, os.Stderr, nil, cmd.MCPCmd{DocsURL: branding.DocsURL, Flags: flags})
}

// WithToken returns a context whose tool calls use a cloud profile that
// authenticates with token.
func WithToken(ctx context.Context, token string) context.Context {
	cfg := &config.Config{Profiles: map[string]config.Profile{
		config.DefaultProfile: {
			Name:         config.DefaultProfile,
			Type:         config.ProfileTypeCloud,
			Token:        token,
			ControlPlane: controlplane.DefaultEndpoint,
		},
	}}
	cfg.OverrideCurrentProfile(config.DefaultProfile)
	return config.WithConfig(ctx, cfg)
}
