// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/docker/go-units"
	buildapi "unikraft.com/cloud/sdk/build"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/cmd"
)

type BuilderCmd struct {
	cmd.ModelCmd[Builder]

	Create BuilderCreateCmd `cmd:"" help:"Create a builder that remote builds run on."`
	Get    BuilderGetCmd    `cmd:"" help:"Inspect a builder." aliases:"inspect,show"`
	List   BuilderListCmd   `cmd:"" help:"List builders." aliases:"ls"`
	Delete BuilderDeleteCmd `cmd:"" help:"Delete a builder and its cache." aliases:"rm,remove"`
	Du     BuilderDuCmd     `cmd:"" help:"Show the disk usage of a builder's cache."`
	Prune  BuilderPruneCmd  `cmd:"" help:"Free a builder's cache, keeping the builder."`
}

// Builder is a builder of the build service, and the builds the service runs on it.
type Builder struct {
	Name      string         `mirror:"status.name" field:",short" help:"Name of the builder."`
	IsDefault bool           `mirror:"status.default" field:"default,short" help:"Whether builds that name no builder run on it."`
	Running   int            `mirror:"status.builds.#" field:",short" help:"Number of builds running on the builder."`
	Waiting   int32          `mirror:"status.waiting_builds" field:",short" help:"Number of builds waiting for the builder, or for any builder of the pool for default."`
	MaxBuilds int32          `mirror:"status.max_builds" field:",short" help:"Number of builds that run on the builder at once."`
	Images    []string       `field:",long" help:"Images that the running builds push."`
	Instance  Link[Instance] `mirror:"status.instance" field:",long" help:"Instance the builder runs as, for its image, sizes and state."`

	Status buildapi.BuilderStatus `field:"-" json:"status"`
}

func (Builder) Type() model.Type {
	return model.Type{
		Name:  "builder",
		Names: "builders",
	}
}

func (b Builder) Key() model.Key {
	return staticKey(b.Name)
}

func (b Builder) Raw() any {
	return b.Status
}

func (b Builder) Fields(ctx context.Context) ([]model.Field, error) {
	return model.FieldsFromStruct(b)
}

func (Builder) Get(ctx context.Context, keys []string) ([]model.Model, error) {
	client, profile, err := builderClientOf(ctx)
	if err != nil {
		return nil, err
	}
	var results []model.Model
	for _, name := range keys {
		s, err := client.GetBuilder(ctx, name)
		if err != nil {
			return results, builderError(err, name)
		}
		b, err := builderOf(profile, *s)
		if err != nil {
			return results, err
		}
		results = append(results, b)
	}
	return results, nil
}

func (Builder) Default(ctx context.Context) (model.Model, error) {
	results, err := Builder{}.Get(ctx, []string{"default"})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func (Builder) List(ctx context.Context) ([]model.Model, error) {
	client, profile, err := builderClientOf(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := client.ListBuilders(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]model.Model, 0, len(resp.Builders))
	for _, s := range resp.Builders {
		b, err := builderOf(profile, s)
		if err != nil {
			return nil, err
		}
		results = append(results, b)
	}
	return results, nil
}

func (Builder) Delete(ctx context.Context, keys []string) error {
	client, _, err := builderClientOf(ctx)
	if err != nil {
		return err
	}
	for _, name := range keys {
		if err := client.DeleteBuilder(ctx, name); err != nil {
			return builderError(err, name)
		}
	}
	return nil
}

func (Builder) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Show the builds on the builder of remote builds, and its instance",
				Commands:    []string{"unikraft builder get --remote https://build.example.com"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List the builders of the build service",
				Commands:    []string{"unikraft builder list --remote https://build.example.com"},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a builder and its cache",
				Commands:    []string{"unikraft builder delete ci --remote https://build.example.com"},
			},
		},
	}
}

// builderOf returns the builder of s, linking to its instance in the profile's metro of that endpoint.
func builderOf(profile *config.Profile, s buildapi.BuilderStatus) (Builder, error) {
	b := Builder{Status: s}
	if err := mirror.Mirror(b, &b); err != nil {
		return Builder{}, fmt.Errorf("could not mirror builder data: %w", err)
	}
	for _, metro := range profile.Metros {
		if metro.Endpoint == s.Instance.Metro {
			b.Instance.Metro = metro.Name
		}
	}
	for _, build := range s.Builds {
		for _, out := range build.Output {
			if out.Image != nil {
				b.Images = append(b.Images, *out.Image)
			}
		}
	}
	return b, nil
}

// builderRemoteKey holds the URL of the build service for the methods of Builder.
type builderRemoteKey struct{}

type BuilderGetCmd struct {
	cmd.ModelGetCmd[Builder]
	Remote string `env:"UNIKRAFT_BUILD_URL" placeholder:"url" help:"URL of the build service that manages the builder. Defaults to the build_url of the profile."`
}

func (c *BuilderGetCmd) Run(ctx context.Context, stdio config.Stdio) error {
	return c.ModelGetCmd.Run(context.WithValue(ctx, builderRemoteKey{}, c.Remote), stdio, nil)
}

type BuilderListCmd struct {
	cmd.ModelListCmd[Builder]
	Remote string `env:"UNIKRAFT_BUILD_URL" placeholder:"url" help:"URL of the build service that manages the builders. Defaults to the build_url of the profile."`
}

func (c *BuilderListCmd) Run(ctx context.Context, stdio config.Stdio) error {
	return c.ModelListCmd.Run(context.WithValue(ctx, builderRemoteKey{}, c.Remote), stdio, nil)
}

type BuilderDeleteCmd struct {
	cmd.ModelRemoveCmd[Builder]
	Remote string `env:"UNIKRAFT_BUILD_URL" placeholder:"url" help:"URL of the build service that manages the builder. Defaults to the build_url of the profile."`
}

func (c *BuilderDeleteCmd) Run(ctx context.Context, stdio config.Stdio) error {
	return c.ModelRemoveCmd.Run(context.WithValue(ctx, builderRemoteKey{}, c.Remote), stdio, nil)
}

type BuilderCreateCmd struct {
	Name   string `arg:"" optional:"" default:"default" help:"Name of the builder."`
	Remote string `env:"UNIKRAFT_BUILD_URL" placeholder:"url" help:"URL of the build service that manages the builder. Defaults to the build_url of the profile."`
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
	Remote string `env:"UNIKRAFT_BUILD_URL" placeholder:"url" help:"URL of the build service that manages the builder. Defaults to the build_url of the profile."`
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
	Remote string `env:"UNIKRAFT_BUILD_URL" placeholder:"url" help:"URL of the build service that manages the builder. Defaults to the build_url of the profile."`
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
	url = cmp.Or(url, profile.BuildURL)
	if url == "" {
		return nil, fmt.Errorf("no build service: set build_url in profile %s, or pass --remote", profile.Name)
	}
	return buildapi.New(url, buildapi.WithToken(profile.Token))
}

// builderClientOf returns the client of the build service in ctx, and the current profile.
func builderClientOf(ctx context.Context) (*buildapi.Client, *config.Profile, error) {
	cfg := config.G(ctx)
	url, _ := ctx.Value(builderRemoteKey{}).(string)
	client, err := builderClient(cfg, url)
	if err != nil {
		return nil, nil, err
	}
	profile, err := cfg.CurrentProfile()
	return client, profile, err
}

// builderError tells how to create the named builder when the service has none.
func builderError(err error, name string) error {
	var apiErr *buildapi.APIError
	if errors.As(err, &apiErr) && apiErr.Code == buildapi.ErrorCodeNotFound {
		return fmt.Errorf("there is no builder %s: create it with unikraft builder create %s", name, name)
	}
	return err
}
