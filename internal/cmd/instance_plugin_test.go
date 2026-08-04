// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd_test

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/cmd"
	resourcecmd "unikraft.com/cli/internal/resource/cmd"
)

func TestInstancePluginUnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    cmd.InstancePlugin
		wantErr bool
	}{
		{
			name:  "name and rom only",
			input: "name=sandbox,rom=sandbox:latest",
			want:  cmd.InstancePlugin{Name: "sandbox", Rom: "sandbox:latest"},
		},
		{
			name:  "name rom and config",
			input: `name=sandbox,rom=sandbox:latest,config={"debug":true,"port":8080}`,
			want:  cmd.InstancePlugin{Name: "sandbox", Rom: "sandbox:latest", Config: `{"debug":true,"port":8080}`},
		},
		{
			name:  "config with nested json",
			input: `name=sandbox,rom=sandbox:latest,config={"db":{"host":"localhost","port":5432}}`,
			want:  cmd.InstancePlugin{Name: "sandbox", Rom: "sandbox:latest", Config: `{"db":{"host":"localhost","port":5432}}`},
		},
		{
			name:  "config with commas and colons",
			input: `name=sandbox,rom=sandbox:latest,config={"a":1,"b":"x,y","c":"k:v"}`,
			want:  cmd.InstancePlugin{Name: "sandbox", Rom: "sandbox:latest", Config: `{"a":1,"b":"x,y","c":"k:v"}`},
		},
		{
			name:  "name only",
			input: "name=sandbox",
			want:  cmd.InstancePlugin{Name: "sandbox"},
		},
		{
			name:  "whitespace around values",
			input: "name= sandbox , rom= sandbox:latest ",
			want:  cmd.InstancePlugin{Name: "sandbox", Rom: "sandbox:latest"},
		},
		{
			name:    "empty string",
			input:   "",
			want:    cmd.InstancePlugin{},
			wantErr: false,
		},
		{
			name:    "missing value",
			input:   "name=",
			want:    cmd.InstancePlugin{Name: ""},
			wantErr: false,
		},
		{
			name:    "unknown field",
			input:   "name=sandbox,unknown=val",
			wantErr: true,
		},
		{
			name:  "no equals sign treated as empty value",
			input: "name",
			want:  cmd.InstancePlugin{Name: ""},
		},
		{
			name:  "rom only",
			input: "rom=sandbox:latest",
			want:  cmd.InstancePlugin{Rom: "sandbox:latest"},
		},
		{
			name:  "config only",
			input: `config={"key":"value"}`,
			want:  cmd.InstancePlugin{Config: `{"key":"value"}`},
		},
		{
			name:  "config is last and consumes rest of string",
			input: `name=sandbox,rom=sandbox:latest,config=trailing`,
			want:  cmd.InstancePlugin{Name: "sandbox", Rom: "sandbox:latest", Config: "trailing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p cmd.InstancePlugin
			err := p.UnmarshalText([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, p)
			}
		})
	}
}

func TestInstancePluginShortcutFlag(t *testing.T) {
	type cli struct {
		Plugin []cmd.InstancePlugin `shortcut:"plugins" sep:"none"`
	}

	t.Run("single plugin", func(t *testing.T) {
		var c cli
		flags := parsePluginFlags(t, &c, `--plugin=name=sandbox,rom=sandbox:latest`)

		var args resourcecmd.SetArgs
		err := resourcecmd.ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		require.Len(t, args.Set, 1)
		assert.Equal(t, "name=sandbox, rom=sandbox:latest", args.Set[0]["plugins"])
	})

	t.Run("multiple plugins", func(t *testing.T) {
		var c cli
		flags := parsePluginFlags(t, &c,
			`--plugin=name=sandbox,rom=sandbox:latest`,
			`--plugin=name=logger,rom=logger:latest`,
		)

		var args resourcecmd.SetArgs
		err := resourcecmd.ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		require.Len(t, args.Set, 2)
	})

	t.Run("plugin with config containing commas", func(t *testing.T) {
		var c cli
		flags := parsePluginFlags(t, &c,
			`--plugin=name=sandbox,rom=sandbox:latest,config={"debug":true,"port":8080}`,
		)

		var args resourcecmd.SetArgs
		err := resourcecmd.ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		require.Len(t, args.Set, 1)
		val := args.Set[0]["plugins"]
		assert.Contains(t, val, `config={"debug":true,"port":8080}`)
	})

	t.Run("unset flag produces no entries", func(t *testing.T) {
		var c cli
		flags := parsePluginFlags(t, &c)

		var args resourcecmd.SetArgs
		err := resourcecmd.ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Nil(t, args.Set)
	})
}

func parsePluginFlags(t *testing.T, cli any, args ...string) []*kong.Flag {
	t.Helper()
	parser, err := kong.New(cli)
	require.NoError(t, err)
	kctx, err := parser.Parse(args)
	require.NoError(t, err)
	return kctx.Flags()
}
