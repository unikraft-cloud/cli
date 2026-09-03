// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd_test

import (
	"encoding/json"
	"testing"

	"github.com/go-json-experiment/json/jsontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/mirror"
)

func TestInstancePluginUnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    cmd.InstancePlugin
		wantErr string
	}{
		{
			name: "name and rom",
			in:   "name=sandbox,rom=plugins/sandbox:latest",
			want: cmd.InstancePlugin{Name: "sandbox", Rom: "plugins/sandbox:latest"},
		},
		{
			name: "object config",
			in:   `name=logger,rom=plugins/logger:latest,config={"level":"debug"}`,
			want: cmd.InstancePlugin{Name: "logger", Rom: "plugins/logger:latest", Config: `{"level":"debug"}`},
		},
		{
			name: "config keeps its own commas",
			in:   `name=logger,rom=r:1,config={"a":1,"tags":["x","y"],"msg":"p,q"}`,
			want: cmd.InstancePlugin{Name: "logger", Rom: "r:1", Config: `{"a":1,"tags":["x","y"],"msg":"p,q"}`},
		},
		{
			name: "scalar config",
			in:   "name=logger,rom=r:1,config=5",
			want: cmd.InstancePlugin{Name: "logger", Rom: "r:1", Config: "5"},
		},
		{
			name:    "missing name is rejected",
			in:      "rom=plugins/logger:latest",
			wantErr: "must specify name= for a plugin",
		},
		{
			name:    "missing rom is rejected",
			in:      "name=logger",
			wantErr: `must specify rom= for plugin "logger"`,
		},
		{
			name:    "config alone is rejected",
			in:      `config={"level":"debug"}`,
			wantErr: "must specify name= for a plugin",
		},
		{
			name:    "empty is rejected",
			in:      "",
			wantErr: "must specify name= for a plugin",
		},
		{
			name:    "truncated config is reported by the splitter",
			in:      "name=logger,rom=r:1,config={oops",
			wantErr: `missing "}"`,
		},
		{
			name:    "malformed config is rejected at parse time",
			in:      "name=logger,rom=r:1,config={oops}",
			wantErr: "not valid JSON",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got cmd.InstancePlugin
			err := got.UnmarshalText([]byte(tt.in))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInstancePluginJSONRequiresFields(t *testing.T) {
	for _, in := range []string{
		`{"config":{"level":"debug"}}`,
		`{"name":"logger"}`,
		`{"rom":"plugins/logger:latest"}`,
		`"config={\"level\":\"debug\"}"`,
	} {
		var p cmd.InstancePlugin
		assert.ErrorContains(t, json.Unmarshal([]byte(in), &p), "must specify", "input %s", in)
	}
}

func TestInstancePluginConfigMirror(t *testing.T) {
	tests := []struct {
		name       string
		config     any
		wantConfig cmd.PluginConfig
	}{
		{"object serialized by the platform", `{"level":"debug"}`, `{"level":"debug"}`},
		{"array serialized by the platform", "[1,2]", "[1,2]"},
		{"number serialized by the platform", "30", "30"},
		{"string serialized by the platform", `"debug"`, `"debug"`},
		{"structured object", map[string]any{"level": "debug"}, `{"level":"debug"}`},
		{"absent", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := platform.InstancePlugin{
				Name: "logger",
				AdditionalProperties: map[string]jsontext.Value{
					"image": jsontext.Value(`"plugins/logger:latest"`),
				},
			}
			if tt.config != nil {
				cfg := tt.config
				plugin.Config = &cfg
			}
			result := cmd.Instance{Instance: platform.Instance{
				Name:    "demo-instance",
				Uuid:    "instance-uuid-1234",
				State:   platform.InstanceStateRunning,
				Image:   "nginx:latest",
				Plugins: []platform.InstancePlugin{plugin},
			}}

			require.NoError(t, mirror.Mirror(result, &result))
			require.Len(t, result.Plugins, 1)
			assert.Equal(t, "logger", result.Plugins[0].Name)
			assert.Equal(t, "plugins/logger:latest", result.Plugins[0].Rom)
			assert.Equal(t, tt.wantConfig, result.Plugins[0].Config)
		})
	}
}
