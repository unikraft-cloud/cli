// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/patch"
	"unikraft.com/cli/pkg/types"
)

func TestJasonFlags(t *testing.T) {
	for _, tt := range []struct {
		name         string
		args         []string
		wantScale    InstanceScaleToZero
		wantAutokill InstanceAutokill
	}{
		{
			name:      "shorthand",
			args:      []string{"--scale-to-zero", "on"},
			wantScale: InstanceScaleToZero{Policy: "on"},
		},
		{
			name:      "csv",
			args:      []string{"--scale-to-zero", "policy=idle,stateful=true,cooldown-time=300"},
			wantScale: InstanceScaleToZero{Policy: "idle", Stateful: true, CooldownTime: 300},
		},
		{
			name:      "json",
			args:      []string{"--scale-to-zero", `{"policy":"on","notify-time":"1s"}`},
			wantScale: InstanceScaleToZero{Policy: "on", NotifyTime: 1000},
		},
		{
			name:      "items",
			args:      []string{"--scale-to-zero", "policy=idle", "stateful=true", "cooldown-time=300ms"},
			wantScale: InstanceScaleToZero{Policy: "idle", Stateful: true, CooldownTime: 300},
		},
		{
			name:         "items for several flags",
			args:         []string{"--scale-to-zero", "policy=on", "cooldown-time=5s", "--autokill", "time=1m", "num-requests=3"},
			wantScale:    InstanceScaleToZero{Policy: "on", CooldownTime: 5000},
			wantAutokill: InstanceAutokill{TimeMs: types.DurationMS(60_000), NumRequests: 3},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var cli UnikraftCLI
			parser, err := NewParser(&cli)
			require.NoError(t, err)

			args := append([]string{"instance", "create", "--image", "nginx:latest"}, tt.args...)
			_, err = parser.Parse(append(args, "--name", "my-instance"))
			require.NoError(t, err)

			spec := patch.PatchSpec{Create: true}
			require.NoError(t, cli.Instances.Create.GeneratedFlags().Apply(&spec))
			if tt.wantScale != (InstanceScaleToZero{}) {
				assert.Equal(t, tt.wantScale, spec.SetTyped["scale-to-zero"])
			}
			if tt.wantAutokill != (InstanceAutokill{}) {
				assert.Equal(t, tt.wantAutokill, spec.SetTyped["autokill"])
			}
		})
	}
}

func TestJasonCollectionFlags(t *testing.T) {
	parse := func(t *testing.T, args ...string) *cmd.FlagSet {
		t.Helper()
		var cli UnikraftCLI
		parser, err := NewParser(&cli)
		require.NoError(t, err)
		kctx, err := parser.Parse(args)
		require.NoError(t, err)
		creator, ok := kctx.Selected().Target.Addr().Interface().(interface{ GeneratedFlags() *cmd.FlagSet })
		require.True(t, ok)
		return creator.GeneratedFlags()
	}
	typed := func(t *testing.T, set *cmd.FlagSet) map[string]any {
		t.Helper()
		spec := patch.PatchSpec{Create: true}
		require.NoError(t, set.Apply(&spec))
		return spec.SetTyped
	}

	t.Run("instance", func(t *testing.T) {
		got := typed(t, parse(t, "instance", "create", "--image", "nginx:latest",
			"-e", "A=1", "B=x,y", "-e", "C=2",
			"--annotation", "x=1", "y=2",
			"--volume", "name=data", "at=/data", "readonly=true", "--volume", "logs:/logs:ro",
			"--rom", "image=r:1", "at=/mnt/x",
			"--plugin", "name=p", "image=i", `config:={"a":1}`,
			"--service", "name=sg", "soft-limit=5",
			"-p", "source=443", "destination=8080", "handlers[]=http", "handlers[]=tls", "-p", "80:8080/http",
			"--domain", "fqdn=a.com", "certificate=c",
		))

		assert.Equal(t, map[string]string{"A": "1", "B": "x,y", "C": "2"}, got["runtime.env"])
		assert.Equal(t, map[string]string{"x": "1", "y": "2"}, got["annotations"])
		assert.Equal(t, []*InstanceVolume{
			{Name: "data", At: "/data", Readonly: true},
			{Name: "logs", At: "/logs", Readonly: true},
		}, got["volumes"])
		assert.Equal(t, []*InstanceRom{{Name: "mnt-x", Image: "r:1", At: "/mnt/x"}}, got["roms"])
		assert.Equal(t, []*InstancePlugin{{Name: "p", Image: "i", Config: `{"a":1}`}}, got["plugins"])
		assert.Equal(t, &InstanceService{Name: "sg", SoftLimit: 5}, got["service"])
		assert.Equal(t, []*Service{
			{Source: 443, Destination: 8080, Handlers: []platform.ConnectionHandler{"http", "tls"}},
			{Source: 80, Destination: 8080, Handlers: []platform.ConnectionHandler{"http"}},
		}, got["service.services"])
		assert.Equal(t, []Domain{{FQDN: "a.com", Certificate: TextLink[Certificate]{Name: "c"}}}, got["service.domains"])
	})

	t.Run("service", func(t *testing.T) {
		got := typed(t, parse(t, "service", "create",
			"--service", "source=443", "destination=8080", "handlers[]=tls",
			"--domain", "fqdn=a.com", "--domain", "b.com",
			"--autokill", "time=5m",
		))

		assert.Equal(t, []*Service{{Source: 443, Destination: 8080, Handlers: []platform.ConnectionHandler{"tls"}}}, got["services"])
		assert.Equal(t, []Domain{{FQDN: "a.com"}, {FQDN: "b.com"}}, got["domains"])
		assert.Equal(t, Autokill{TimeMs: 300_000}, got["autokill"])
	})
}
