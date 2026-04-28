// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/MakeNowJust/heredoc"
	"github.com/alecthomas/kong"
	jujuerrors "github.com/juju/errors"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/tui/selector"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
)

type ProfileCmd struct {
	cmd.ResourceCmd[Profile]
	cmd.GettableResourceCmd[Profile]
	cmd.ListableResourceCmd[Profile]
	cmd.DeletableResourceCmd[Profile]

	Create ProfileCreateCmd `cmd:"" help:"Create a profile."`
	Use    UseCmd           `cmd:"" help:"Switch between profiles."`
}

// ProfileCreateCmd extends the generic create command with shortcut flags.
type ProfileCreateCmd struct {
	cmd.ResourceCreateCmd[Profile]

	Name         string `group:"flag-create" shortcut:"name" short:"n" help:"Profile name." placeholder:"name"`
	Token        string `group:"flag-create" shortcut:"token" help:"Authentication token." placeholder:"token"`
	Organization string `group:"flag-create" shortcut:"organization" help:"Organization name." placeholder:"org"`
	ControlPlane string `group:"flag-create" name:"controlplane" shortcut:"control-plane" help:"Control plane URL." placeholder:"url" example:"https://controlplane.unikraft.cloud"`
	Insecure     *bool  `group:"flag-create" shortcut:"insecure" short:"k" help:"Allow insecure connections."`
}

func (c *ProfileCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

type Profile struct {
	Name         string `field:",short" json:"name" create:"set,required"`
	Active       bool   `field:",short" json:"active"`
	Organization string `field:",short" json:"organization" create:"set"`
	Token        string `field:",hidden" json:"token" create:"set,required"`
	ControlPlane string `field:",long" json:"controlplane" create:"set"`
	Insecure     *bool  `field:",long" json:"insecure" create:"set"`

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

func (i Profile) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

func (Profile) List(ctx context.Context) ([]resource.Resource, error) {
	cfg := config.G(ctx)
	profiles := cfg.Profiles

	var results []resource.Resource
	for _, profile := range profiles {
		metroNames := make([]string, 0, len(profile.Metros))
		for _, metro := range profile.Metros {
			metroNames = append(metroNames, metro.Name)
		}

		result := Profile{
			Name:         profile.Name,
			Active:       profile.Name == cfg.DefaultProfile,
			Organization: profile.Organization,
			Token:        profile.Token,
			ControlPlane: profile.ControlPlane,
			Insecure:     &profile.Insecure,
			Metros:       metroNames,
		}
		results = append(results, result)
	}
	slices.SortFunc(results, func(a, b resource.Resource) int {
		return cmp.Compare(a.(Profile).Name, b.(Profile).Name)
	})
	return results, nil
}

func (Profile) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	return getFromListable(ctx, Profile{}, keys)
}

func (Profile) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	cfg := config.FromContextOrDefault(ctx)

	var name, token, organization, controlPlane string
	var insecure *bool
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		switch key.String() {
		case "name":
			name = field.Create.Set.(string)
		case "token":
			token = field.Create.Set.(string)
		case "organization":
			organization = field.Create.Set.(string)
		case "control-plane":
			controlPlane = field.Create.Set.(string)
		case "insecure":
			v := field.Create.Set.(bool)
			insecure = &v
		}
	}

	if _, ok := cfg.Profiles[name]; ok {
		return nil, fmt.Errorf("profile already exists: %s", name)
	}

	profile := config.Profile{
		Name:         name,
		Type:         config.ProfileTypeCloud,
		Token:        token,
		Organization: organization,
		ControlPlane: controlPlane,
		Insecure:     insecure != nil && *insecure,
	}
	cfg.AddProfile(profile)
	// Set as default profile if this is the first one or no default is set.
	if cfg.DefaultProfile == "" || len(cfg.Profiles) == 1 {
		cfg.DefaultProfile = name
	}
	if err := cfg.Save(); err != nil {
		return nil, err
	}

	return []resource.Resource{Profile{
		Name:   name,
		Active: name == cfg.CurrentProfileName(),
	}}, nil
}

func (Profile) Delete(ctx context.Context, targets []resource.Resource) error {
	cfg := config.FromContextOrDefault(ctx)
	currentName := cfg.CurrentProfileName()

	for _, target := range targets {
		p := target.(Profile)
		if p.Name == currentName {
			return fmt.Errorf("cannot delete the active profile: %s", p.Name)
		}
		delete(cfg.Profiles, p.Name)
	}

	return cfg.Save()
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
		cmd.CmdTypeCreate: {
			{
				Description: "Create a new profile",
				Commands: []string{
					`unikraft profile create \
  --name staging \
  --token mytoken \
  --organization my-org`,
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a profile",
				Commands:    []string{"unikraft profile delete staging"},
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
