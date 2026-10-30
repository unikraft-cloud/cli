// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/go-units"
	buildapi "unikraft.com/cloud/sdk/build"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
)

type BuilderCmd struct {
	Create BuilderCreateCmd `cmd:"" help:"Create a builder that remote builds run on."`
	Du     BuilderDuCmd     `cmd:"" help:"Show the disk usage of a builder's cache."`
	Prune  BuilderPruneCmd  `cmd:"" help:"Free a builder's cache, keeping the builder."`
}

type BuilderCreateCmd struct {
	Name   string `arg:"" optional:"" default:"default" help:"Name of the builder."`
	Remote string `env:"UNIKRAFT_BUILD_URL" required:"" placeholder:"url" help:"URL of the build service that manages the builder."`
}

func (BuilderCreateCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Create the builder of remote builds",
			Commands:    []string{"unikraft builder create --remote https://build.example.com"},
		},
	}
}

type BuilderDuCmd struct {
	Name   string `arg:"" optional:"" default:"default" help:"Name of the builder."`
	Remote string `env:"UNIKRAFT_BUILD_URL" required:"" placeholder:"url" help:"URL of the build service that manages the builder."`
}

func (BuilderDuCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Show how much cache the builder of remote builds holds",
			Commands:    []string{"unikraft builder du --remote https://build.example.com"},
		},
	}
}

type BuilderPruneCmd struct {
	Name   string `arg:"" optional:"" default:"default" help:"Name of the builder."`
	Remote string `env:"UNIKRAFT_BUILD_URL" required:"" placeholder:"url" help:"URL of the build service that manages the builder."`
}

func (BuilderPruneCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Free the cache of the builder of remote builds",
			Commands:    []string{"unikraft builder prune --remote https://build.example.com"},
		},
	}
}

func (c *BuilderCreateCmd) Run(ctx context.Context, cfg *config.Config) error {
	client, err := builderClient(cfg, c.Remote)
	if err != nil {
		return err
	}
	if _, err := client.CreateBuilder(ctx, buildapi.Builder{Name: &c.Name}); err != nil {
		return err
	}
	log.G(ctx).Info().Str("name", c.Name).Msg("builder ready")
	return nil
}

func (c *BuilderDuCmd) Run(ctx context.Context, cfg *config.Config, stdio config.Stdio) error {
	client, err := builderClient(cfg, c.Remote)
	if err != nil {
		return err
	}
	u, err := client.GetBuilderUsage(ctx, c.Name)
	if err != nil {
		return builderError(err, c.Name)
	}
	_, err = fmt.Fprintf(stdio.Stdout, "Reclaimable: %s\nTotal:       %s\n", units.BytesSize(float64(u.ReclaimableBytes)), units.BytesSize(float64(u.TotalBytes)))
	return err
}

func (c *BuilderPruneCmd) Run(ctx context.Context, cfg *config.Config) error {
	client, err := builderClient(cfg, c.Remote)
	if err != nil {
		return err
	}
	resp, err := client.PruneBuilder(ctx, c.Name)
	if err != nil {
		return builderError(err, c.Name)
	}
	log.G(ctx).Info().Str("freed", units.BytesSize(float64(resp.FreedBytes))).Msg("pruned the builder's cache")
	return nil
}

// builderClient returns the client of the build service at url, as the current profile.
func builderClient(cfg *config.Config, url string) (*buildapi.Client, error) {
	profile, err := cfg.CurrentProfile()
	if err != nil {
		return nil, err
	}
	return buildapi.New(url, buildapi.WithToken(profile.Token))
}

// builderError tells how to create the named builder when the service has none.
func builderError(err error, name string) error {
	var apiErr *buildapi.APIError
	if errors.As(err, &apiErr) && apiErr.Code == buildapi.ErrorCodeNotFound {
		return fmt.Errorf("there is no builder %s: create it with unikraft builder create %s", name, name)
	}
	return err
}
