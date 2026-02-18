// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"

	jujuerrors "github.com/juju/errors"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
)

type ProfileCmd struct {
	cmd.ResourceCmd[Profile]
	cmd.GettableResourceCmd[Profile] `set:"name=profile" set:"names=profiles"`
	cmd.ListableResourceCmd[Profile] `set:"name=profile" set:"names=profiles"`

	Use UseCmd `cmd:"" help:"Switch between profiles."`
}

type Profile struct {
	Name   string `field:",short" json:"name"`
	Active bool   `field:",short" json:"active"`

	Metros []string `field:",short" json:"metros"`
}

func (Profile) Type() resource.Type {
	return resource.Type{
		Name:  "profile",
		Names: "profiles",
	}
}

func (i Profile) Key() resource.Key {
	return staticKey(i.Name)
}

func (i Profile) Raw() any {
	return i
}

func (i Profile) Fields() ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

func (Profile) List(ctx context.Context) ([]resource.Resource, error) {
	cfg := config.G(ctx)
	profiles := cfg.Profiles

	var results []resource.Resource
	for _, profile := range profiles {
		metroNames := make([]string, 0, len(profile.Metros))
		for _, metro := range profile.Metros {
			metroNames = append(metroNames, metro.Name.String())
		}

		result := Profile{
			Name:   profile.Name,
			Active: profile.Name == cfg.DefaultProfile.String(),
			Metros: metroNames,
		}
		results = append(results, result)
	}
	return results, nil
}

func (Profile) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	return getFromListable(ctx, Profile{}, keys)
}

func (Profile) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a profile by name",
				Commands:    []string{"unikraft profile get default"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all profiles",
				Commands:    []string{"unikraft profile list"},
			},
		},
	}
}

type UseCmd struct {
	Name string `arg:"" help:"Name of the profile to switch to."`
}

func (cmd *UseCmd) Run(ctx context.Context, cfg *config.Config) error {
	_, ok := cfg.Profiles[cmd.Name]
	if !ok {
		return config.ErrProfileNotFound{Name: cmd.Name}
	}
	cfg.DefaultProfile = config.InterpolateString(cmd.Name)

	if err := cfg.Save(); err != nil {
		return jujuerrors.Annotate(err, "saving profile")
	}

	log.G(ctx).Info().
		Str("profile", cmd.Name).
		Msg("switched profile")
	return nil
}
