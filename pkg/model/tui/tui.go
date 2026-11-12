// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"unikraft.com/cli/internal/tui/uitui"
	"unikraft.com/cli/pkg/model"
)

// New builds the TUI for the models in registry. It opens on the home panel,
// the list of the resource type, or the resource with that name.
func New(ctx context.Context, registry *model.Registry, home Home, resource, name string) (tea.Model, error) {
	var panel tea.Model
	switch {
	case name != "":
		if resource == "" {
			return nil, fmt.Errorf("resource type must be specified when providing a name")
		}
		selected, ok := registry.Resolve(resource)
		if !ok {
			return nil, fmt.Errorf("unknown resource: %s", resource)
		}
		if selected.Get == nil {
			return nil, fmt.Errorf("resource %s does not support get", selected.Name)
		}
		panel = NewDetailPanel(ctx, registry, selected, name)
	case resource != "":
		selected, ok := registry.Resolve(resource)
		if !ok {
			return nil, fmt.Errorf("unknown resource: %s", resource)
		}
		panel = NewListPanel(ctx, registry, selected)
	default:
		panel = NewHomePanel(ctx, registry, home)
	}
	return uitui.NewModel(panel), nil
}
