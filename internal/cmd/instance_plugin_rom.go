// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"maps"
	"slices"
	"strings"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/log"
)

// HACK: metros that predate the plugin image field still want it as rom, so a
// request they reject for that is sent once more with rom. This can go once
// they have all been bumped.
func pluginRomRejected(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Missing member 'rom'")
}

func warnPluginRom(ctx context.Context, metro string) {
	log.G(ctx).Warn().Str("metro", metro).Msg("metro does not accept image= for plugins yet, retrying with rom=")
}

func pluginPatchesWithRom(reqs []platform.UpdateInstancesRequestItem) ([]platform.UpdateInstancesRequestItem, bool) {
	rewritten := false
	reqs = slices.Clone(reqs)
	for i, req := range reqs {
		if req.Prop != platform.MutableInstancePropertyPlugins || req.Value == nil {
			continue
		}
		plugins, ok := (*req.Value).([]map[string]any)
		if !ok {
			continue
		}
		withRom := make([]map[string]any, 0, len(plugins))
		for _, plugin := range plugins {
			plugin = maps.Clone(plugin)
			if image, ok := plugin["image"]; ok {
				plugin["rom"] = image
				delete(plugin, "image")
				rewritten = true
			}
			withRom = append(withRom, plugin)
		}
		reqs[i].Value = new(any(withRom))
	}
	return reqs, rewritten
}

func createPluginsWithRom(req platform.CreateInstanceRequest) (platform.CreateInstanceRequest, bool) {
	rewritten := false
	req.Plugins = slices.Clone(req.Plugins)
	for i := range req.Plugins {
		if req.Plugins[i].Image == nil {
			continue
		}
		req.Plugins[i].Rom, req.Plugins[i].Image = req.Plugins[i].Image, nil //nolint:staticcheck // The deprecated field is what old metros want.
		rewritten = true
	}
	return req, rewritten
}
