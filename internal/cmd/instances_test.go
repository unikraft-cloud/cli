// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd_test

import (
	"encoding/json"
	"testing"

	xjson "github.com/go-json-experiment/json"
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
			name:    "malformed config is rejected at parse time",
			in:      "name=logger,rom=r:1,config={oops",
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

func TestInstancePluginConfigMirror(t *testing.T) {
	tests := []struct {
		name string
		cfg  any
		want cmd.PluginConfig
	}{
		{"object", map[string]any{"level": "debug"}, `{"level":"debug"}`},
		{"array", []any{1, 2}, "[1,2]"},
		{"number", 30, "30"},
		{"string", "debug", `"debug"`},
		{"absent", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := platform.InstancePlugin{Name: "logger", Rom: "plugins/logger:latest"}
			if tt.cfg != nil {
				cfg := tt.cfg
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
			assert.Equal(t, tt.want, result.Plugins[0].Config)
		})
	}
}

func TestInstancePluginConfigPrecision(t *testing.T) {
	const raw = `{"id":9007199254740993,"ratio":1.0000000000000002}`

	var p cmd.InstancePlugin
	require.NoError(t, p.UnmarshalText([]byte("name=logger,rom=r:1,config="+raw)))

	var config any = jsontext.Value(p.Config)
	req := platform.CreateInstanceRequest{
		Plugins: []platform.CreateInstanceRequestPlugin{{
			Name: p.Name, Rom: platform.ImageReference(p.Rom), Config: &config,
		}},
	}
	out, err := xjson.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"config":`+raw)
}

func TestInstancePluginPatchValue(t *testing.T) {
	var p cmd.InstancePlugin
	require.NoError(t, p.UnmarshalText([]byte(`name=logger,rom=plugins/logger:latest,config={"id":9007199254740993,"level":"debug"}`)))

	reqPlugins := []map[string]any{{
		"name":   p.Name,
		"rom":    p.Rom,
		"config": jsontext.Value(p.Config),
	}}
	var v any = reqPlugins
	out, err := xjson.Marshal(platform.UpdateInstancesRequestItem{
		Prop:  platform.MutableInstancePropertyPlugins,
		Op:    platform.MutableInstanceOperationSet,
		Value: &v,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"prop":"plugins","op":"set","value":[{"name":"logger","rom":"plugins/logger:latest","config":{"id":9007199254740993,"level":"debug"}}]}`, string(out))
	assert.Contains(t, string(out), "9007199254740993")
}

func TestPluginConfigMarshalJSON(t *testing.T) {
	got, err := json.Marshal(cmd.InstancePlugin{
		Name:   "logger",
		Rom:    "plugins/logger:latest",
		Config: `{"level":"debug"}`,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"logger","rom":"plugins/logger:latest","config":{"level":"debug"}}`, string(got))

	got, err = json.Marshal(cmd.InstancePlugin{Name: "sandbox", Rom: "plugins/sandbox:latest"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"sandbox","rom":"plugins/sandbox:latest"}`, string(got))
}
