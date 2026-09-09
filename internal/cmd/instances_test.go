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
			name: "name and image",
			in:   "name=sandbox,image=plugins/sandbox:latest",
			want: cmd.InstancePlugin{Name: "sandbox", Image: "plugins/sandbox:latest"},
		},
		{
			name: "deprecated rom alias",
			in:   "name=sandbox,rom=plugins/sandbox:latest",
			want: cmd.InstancePlugin{Name: "sandbox", Image: "plugins/sandbox:latest"},
		},
		{
			name:    "rom and image together are rejected",
			in:      "name=sandbox,rom=plugins/sandbox:latest,image=plugins/sandbox:latest",
			wantErr: "must specify only one of rom= and image=",
		},
		{
			name: "object config",
			in:   `name=logger,image=plugins/logger:latest,config={"level":"debug"}`,
			want: cmd.InstancePlugin{Name: "logger", Image: "plugins/logger:latest", Config: `{"level":"debug"}`},
		},
		{
			name: "config keeps its own commas",
			in:   `name=logger,image=r:1,config={"a":1,"tags":["x","y"],"msg":"p,q"}`,
			want: cmd.InstancePlugin{Name: "logger", Image: "r:1", Config: `{"a":1,"tags":["x","y"],"msg":"p,q"}`},
		},
		{
			name: "scalar config",
			in:   "name=logger,image=r:1,config=5",
			want: cmd.InstancePlugin{Name: "logger", Image: "r:1", Config: "5"},
		},
		{
			name:    "missing name is rejected",
			in:      "image=plugins/logger:latest",
			wantErr: "must specify name= for a plugin",
		},
		{
			name:    "missing image is rejected",
			in:      "name=logger",
			wantErr: `must specify image= for plugin "logger"`,
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
			in:      "name=logger,image=r:1,config={oops",
			wantErr: `missing "}"`,
		},
		{
			name:    "malformed config is rejected at parse time",
			in:      "name=logger,image=r:1,config={oops}",
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
		`{"image":"plugins/logger:latest"}`,
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
			assert.Equal(t, "plugins/logger:latest", result.Plugins[0].Image)
			assert.Equal(t, tt.wantConfig, result.Plugins[0].Config)
		})
	}
}

func TestInstancePluginJSONRomAlias(t *testing.T) {
	var p cmd.InstancePlugin
	require.NoError(t, json.Unmarshal([]byte(`{"name":"sandbox","rom":"plugins/sandbox:latest"}`), &p))
	assert.Equal(t, cmd.InstancePlugin{Name: "sandbox", Image: "plugins/sandbox:latest"}, p)

	err := json.Unmarshal([]byte(`{"name":"sandbox","rom":"a:1","image":"b:1"}`), &p)
	assert.ErrorContains(t, err, "must specify only one of rom= and image=")
}

func TestInstanceRomName(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantName string
		wantErr  string
	}{
		{name: "explicit name", in: "name=my-rom,image=r:1,at=/data", wantName: "my-rom"},
		{name: "derived from at", in: "image=r:1,at=/mnt/my_data.v2", wantName: "mnt-mydatav2"},
		{name: "explicit name without at", in: "name=raw,image=r:1", wantName: "raw"},
		{name: "neither name nor at", in: "image=r:1", wantErr: "a ROM must specify at least one of name= or at="},
		{name: "at yields no name", in: "image=r:1,at=/", wantErr: `cannot derive a ROM name from at="/"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got cmd.InstanceRom
			err := got.UnmarshalText([]byte(tt.in))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, got.Name)
		})
	}
}
