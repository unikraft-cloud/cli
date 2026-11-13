// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"unikraft.com/cli/internal/branding"
	"unikraft.com/cli/pkg/model"
	modeltui "unikraft.com/cli/pkg/model/tui"
	xio "unikraft.com/x/io"
	xstdio "unikraft.com/x/stdio"
)

// NewTUIModel builds the same Bubble Tea model used by `unikraft tui`.
func NewTUIModel(ctx context.Context, resourceArg, nameArg string) (tea.Model, error) {
	registry := model.NewRegistry()
	registry.Register(Instance{}, Instance{})
	registry.Register(Volume{}, Volume{})
	registry.Register(ServiceGroup{}, ServiceGroup{})
	registry.Register(Certificate{}, Certificate{})
	registry.Register(ImageEntry{}, Image{})
	registry.Register(Metro{}, Metro{})
	registry.Register(Profile{}, Profile{})

	return modeltui.New(ctx, registry, modeltui.Home{
		Art: branding.Art,
		Links: []modeltui.Link{
			{Label: "Website", URL: branding.WebsiteURL},
			{Label: "GitHub", URL: branding.RepoURL},
		},
	}, resourceArg, nameArg)
}

func (cmd *TUICmd) Run(ctx context.Context, stdio xstdio.Stdio) error {
	m, err := NewTUIModel(ctx, cmd.Resource, cmd.Name)
	if err != nil {
		return err
	}

	program := tea.NewProgram(
		m,
		tea.WithInput(stdio.Stdin),
		tea.WithOutput(xio.Unwrap(stdio.Stdout)),
	)
	_, err = program.Run()
	return err
}
