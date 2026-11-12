// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"github.com/alecthomas/kong"
	"unikraft.com/x/kingkong"

	wkong "unikraft.com/cli/internal/w/kong"
)

// NewParser builds the kong parser for cli, a CLI made of model commands.
// groups are its command groups, in the order help shows them, and opts are
// added to the defaults (name, description, vars, etc).
func NewParser(cli any, version string, groups []kong.Group, opts ...kong.Option) (*kong.Kong, error) {
	globalFlagGroup := kong.Group{
		Key:   "flag-global",
		Title: kingkong.Underline("Global flags") + ":",
	}
	flagGroups := []kong.Group{
		{
			Key:   "flag-create",
			Title: kingkong.Underline("Create flags") + ":",
		},
		{
			Key:   "flag-edit",
			Title: kingkong.Underline("Edit flags") + ":",
		},
		globalFlagGroup,
		{
			Key:   "flag-local",
			Title: kingkong.Underline("Subcommand flags") + ":",
		},
	}

	opts = append([]kong.Option{
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact:             true,
			FlagsLast:           true,
			NoExpandSubcommands: true,
		}),
		kong.Help(kingkong.HelpPrinter(version)),
		kong.WithBeforeReset(func(value *kong.Path) error {
			if value == nil || value.App == nil || value.App.Flags == nil {
				return nil
			}
			for _, f := range value.App.Flags {
				if f.Name == "help" {
					f.Group = &globalFlagGroup
				}
			}
			return nil
		}),
		kong.ExplicitGroups(append(flagGroups, groups...)),
		kong.NamedMapper("optional", wkong.Optional()),
		kong.NamedMapper("hostenv", wkong.HostEnv()),
	}, opts...)

	return kong.New(cli, append(opts, kong.PostBuild(func(k *kong.Kong) error {
		return AttachGeneratedFlags(k, opts...)
	}))...)
}
