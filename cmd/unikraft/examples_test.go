// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"iter"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/shell"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/config"
	"unikraft.com/x/kingkong"
)

func TestExamples(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("UNIKRAFT_CONFIG", configPath)

	cfg := &config.Config{
		Path:           configPath,
		DefaultProfile: config.InterpolateString("default"),
		Profiles: map[string]config.Profile{
			"default": {
				Name: "default",
				Type: config.InterpolateString(string(config.ProfileTypeCloud)),
			},
		},
	}
	require.NoError(t, cfg.Save())

	parser, err := cmd.NewParser(&cmd.UnikraftCLI{})
	require.NoError(t, err)
	require.NotNil(t, parser.Model)

	nodes := slices.Collect(iterNodes(parser.Model.Node))
	sort.Slice(nodes, func(i, j int) bool {
		return nodePath(nodes[i]) < nodePath(nodes[j])
	})
	require.NotEmpty(t, nodes, "no nodes found")

	for _, node := range nodes {
		provider, ok := node.Target.Interface().(kingkong.ExamplesProvider)
		if !ok {
			continue
		}
		t.Run(nodePath(node), func(t *testing.T) {
			for _, example := range provider.Examples() {
				for _, command := range example.Commands {
					args, err := shell.Fields(command, func(string) string { return "" })
					require.NoError(t, err)
					require.NotEmpty(t, args, "no args parsed for example")
					if args[0] != unikraftCmd {
						continue
					}
					args = args[1:]

					parser, err := cmd.NewParser(&cmd.UnikraftCLI{})
					require.NoError(t, err)

					ctx, err := parser.Parse(args)
					require.NoError(t, err, "failed to parse: %s", command)
					require.NotNil(t, runnableNode(ctx), "example does not select a runnable command")
				}
			}
		})
	}
}

func iterNodes(root *kong.Node) iter.Seq[*kong.Node] {
	return func(yield func(*kong.Node) bool) {
		var walk func(*kong.Node) bool
		walk = func(node *kong.Node) bool {
			if node == nil {
				return true
			}
			if !yield(node) {
				return false
			}
			for _, child := range node.Children {
				if !walk(child) {
					return false
				}
			}
			return true
		}
		_ = walk(root)
	}
}

func runnableNode(ctx *kong.Context) *kong.Node {
	node := ctx.Selected()
	if node != nil {
		return node
	}
	if len(ctx.Path) == 0 {
		return nil
	}
	selected := ctx.Path[0].Node()
	if selected.Type == kong.ApplicationNode {
		method := getMethod(selected.Target, "Run")
		if method.IsValid() {
			return selected
		}
	}
	return nil
}

func nodePath(node *kong.Node) string {
	if node.Parent == nil {
		return node.Name
	}
	return nodePath(node.Parent) + " " + node.Name
}
