// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	resourcetui "unikraft.com/cli/internal/resource/tui"
	"unikraft.com/cli/internal/tui/uitui"
	"unikraft.com/x/kingkong"
)

type TUICmd struct {
	Resource string `arg:"" optional:"" help:"Resource type to browse."`
	Name     string `arg:"" optional:"" help:"Resource key to open."`
}

func (TUICmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Open the TUI home screen",
			Commands:    []string{"unikraft tui"},
		},
		{
			Description: "Browse instances directly",
			Commands:    []string{"unikraft tui instances"},
		},
		{
			Description: "Open a specific instance detail view",
			Commands:    []string{"unikraft tui instances demo-instance"},
		},
		{
			Description: "Browse volumes",
			Commands:    []string{"unikraft tui volumes"},
		},
	}
}

// NewTUIModel builds the same Bubble Tea model used by `unikraft tui`.
func NewTUIModel(ctx context.Context, resourceArg, nameArg string) (tea.Model, error) {
	registry := resource.NewRegistry()
	registry.Register(Instance{}, Instance{})
	registry.Register(Volume{}, Volume{})
	registry.Register(ServiceGroup{}, ServiceGroup{})
	registry.Register(Certificate{}, Certificate{})
	registry.Register(ImageEntry{}, Image{})
	registry.Register(Metro{}, Metro{})
	registry.Register(Profile{}, Profile{})

	var panel tea.Model
	switch {
	case nameArg != "":
		if resourceArg == "" {
			return nil, fmt.Errorf("resource type must be specified when providing a name")
		}
		selected, ok := registry.Resolve(resourceArg)
		if !ok {
			return nil, fmt.Errorf("unknown resource: %s", resourceArg)
		}
		if selected.Get == nil {
			return nil, fmt.Errorf("resource %s does not support get", selected.Name)
		}
		panel = resourcetui.NewDetailPanel(ctx, registry, selected, nameArg)
	case resourceArg != "":
		selected, ok := registry.Resolve(resourceArg)
		if !ok {
			return nil, fmt.Errorf("unknown resource: %s", resourceArg)
		}
		panel = resourcetui.NewListPanel(ctx, registry, selected)
	default:
		panel = resourcetui.NewHomePanel(ctx, registry)
	}

	return uitui.NewModel(panel), nil
}

func (cmd *TUICmd) Run(ctx context.Context, stdio config.Stdio) error {
	model, err := NewTUIModel(ctx, cmd.Resource, cmd.Name)
	if err != nil {
		return err
	}

	program := tea.NewProgram(
		model,
		tea.WithInput(stdio.Stdin),
		tea.WithOutput(stdio.Stdout),
	)
	_, err = program.Run()
	return err
}
