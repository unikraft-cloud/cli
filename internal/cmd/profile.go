// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/MakeNowJust/heredoc"
	jujuerrors "github.com/juju/errors"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/cmd"
	"unikraft.com/cli/internal/tui/selector"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
)

type ProfileCmd struct {
	cmd.ModelCmd[Profile]
	cmd.GettableModelCmd[Profile]
	cmd.ListableModelCmd[Profile]

	Use UseCmd `cmd:"" help:"Switch between profiles."`
}

type Profile struct {
	Name   string `field:",short" json:"name" help:"Profile name."`
	Active bool   `field:",short" json:"active" help:"Whether this is the default profile, which --profile overrides."`

	Metros []string `field:",short" json:"metros" help:"Metros of the profile."`
}

func (Profile) Type() model.Type {
	return model.Type{
		Name:  "profile",
		Names: "profiles",
	}
}

func (i Profile) Key() model.Key {
	return staticKey(i.Name)
}

func (i Profile) Raw() any {
	return i
}

func (i Profile) Fields(ctx context.Context) ([]model.Field, error) {
	return model.FieldsFromStruct(i)
}

func (Profile) List(ctx context.Context) ([]model.Model, error) {
	cfg := config.G(ctx)
	profiles := cfg.Profiles

	var results []model.Model
	for _, profile := range profiles {
		metroNames := make([]string, 0, len(profile.Metros))
		for _, metro := range profile.Metros {
			metroNames = append(metroNames, metro.Name)
		}

		result := Profile{
			Name:   profile.Name,
			Active: profile.Name == cfg.DefaultProfile,
			Metros: metroNames,
		}
		results = append(results, result)
	}
	slices.SortFunc(results, func(a, b model.Model) int {
		return cmp.Compare(a.(Profile).Name, b.(Profile).Name)
	})
	return results, nil
}

func (Profile) Get(ctx context.Context, keys []string) ([]model.Model, error) {
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
	Name string `arg:"" optional:"" help:"Target profile to switch to."`
}

func (UseCmd) Help() string {
	return heredoc.Doc(`
		Switch between profiles.

		Calling without an argument will prompt you to select a profile from the
		list of available profiles.
	`)
}

func (cmd *UseCmd) Run(ctx context.Context, cfg *config.Config) error {
	name := cmd.Name

	if name == "" {
		selected, err := selector.SingleWithDefault("select a profile", cfg.DefaultProfile, slices.Sorted(maps.Keys(cfg.Profiles))...)
		if err != nil && errors.Is(err, selector.ErrNoOptionSelected) {
			return nil // Just exit with no error if user cancels out of selection.
		} else if err != nil {
			return jujuerrors.Annotate(err, "selecting profile")
		}
		name = string(selected)
	}

	if _, ok := cfg.Profiles[name]; !ok {
		return config.ErrProfileNotFound{Name: name}
	}
	cfg.DefaultProfile = name

	if err := cfg.Save(); err != nil {
		return jujuerrors.Annotate(err, "saving profile")
	}

	log.G(ctx).
		Info().
		Str("profile", name).
		Msg("using")
	return nil
}
