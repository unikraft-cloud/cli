// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/MakeNowJust/heredoc"

	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/version"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcpserver"
	"unikraft.com/cli/internal/resource"
)

type MCPCmd struct {
	ReadOnly bool   `help:"Expose only the tools that cannot change anything: list, image_get and instance_logs."`
	DocsURL  string `name:"docs-url" env:"UNIKRAFT_DOCS_URL" default:"https://unikraft.com/docs/" placeholder:"url" help:"Documentation site served as resources. Pass an empty value to serve none."`
}

func (MCPCmd) Help() string {
	return heredoc.Docf(`
		The server speaks the Model Context Protocol over stdin and stdout, so an AI
		assistant such as Claude Code, Claude Desktop or Cursor can manage Unikraft
		Cloud through typed tools instead of shelling out to the CLI.

		Every resource type gets list, create, edit and delete tools, named like
		%[1]sinstance_list%[1]s or %[1]svolume_create%[1]s; a list tool also fetches specific resources
		when given keys. Instances additionally get %[1]sinstance_control%[1]s for start, stop,
		restart and suspend, and %[1]sinstance_logs%[1]s. The tools use the current profile and
		fan out across its metros exactly like the matching CLI commands do.

		The documentation is served as resources: %[1]sunikraft://docs%[1]s is the table of
		contents and %[1]sunikraft://docs/<path>%[1]s a page, such as %[1]sunikraft://docs/cli/fields%[1]s.
		Pages are fetched from the documentation site when read, so they cost
		nothing until an assistant asks for one.

		Stdout carries the protocol, so diagnostics go to stderr; raise them with
		%[1]s--log-level debug%[1]s. A %[1]s--timeout%[1]s ends the whole session when it expires.

		Register the server with a client as a stdio server, for example:

		    {"mcpServers": {"unikraft": {"command": "unikraft", "args": ["mcp"]}}}
	`, "`")
}

func (MCPCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Serve every tool over stdio",
			Commands: []string{
				"unikraft mcp",
			},
		},
		{
			Description: "Serve only the tools that cannot change anything",
			Commands: []string{
				"unikraft mcp --read-only",
			},
		},
		{
			Description: "Serve another profile with debug logs on stderr",
			Commands: []string{
				"unikraft mcp --profile staging --log-level debug",
			},
		},
	}
}

func (c *MCPCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	srv, err := newMCPServer(ctx, stdio.Stderr, partition, *c)
	if err != nil {
		return err
	}
	log.G(ctx).Info().Bool("read-only", c.ReadOnly).Msg("serving MCP over stdio")
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func newMCPServer(ctx context.Context, stderr io.Writer, partition *resource.Partition, c MCPCmd) (*mcpserver.Server, error) {
	var logger *slog.Logger
	if log.G(ctx).GetLevel() <= log.DebugLevel {
		logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	srv, err := mcpserver.New(ctx, mcpserver.Options{
		Partition: partition,
		ReadOnly:  c.ReadOnly,
		Version:   version.Version,
		Stderr:    stderr,
		Logger:    logger,
		DocsURL:   c.DocsURL,
	})
	if err != nil {
		return nil, err
	}
	if err := errors.Join(
		srv.AddTools[Instance](ctx),
		srv.AddTools[InstanceTemplate](ctx),
		srv.AddTools[InstanceCheckpoint](ctx),
		srv.AddTools[Volume](ctx),
		srv.AddTools[VolumeTemplate](ctx),
		srv.AddTools[ServiceGroup](ctx),
		srv.AddListTool[Certificate](ctx),
		srv.AddCreateTool[Certificate](ctx),
		srv.AddDeleteTool[Certificate](ctx),
		srv.AddListTool[ImageEntry](ctx),
		srv.AddGetTool[Image](ctx),
		srv.AddDeleteTool[Image](ctx),
		srv.AddListTool[Metro](ctx),
		srv.AddListTool[Profile](ctx),
		srv.AddInstanceTools(mcpInstanceCommands{}),
	); err != nil {
		return nil, err
	}
	return srv, nil
}
