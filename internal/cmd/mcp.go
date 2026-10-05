// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"

	"github.com/MakeNowJust/heredoc"

	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/version"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcpserver"
	"unikraft.com/cli/internal/resource"
)

const instanceEditNote = "Edits to tags, delete-lock, autokill, scale-to-zero and sched-priority apply at once, even while the instance runs. Other edits to a running instance are queued until it next stops. Adding plugins, roms or volumes takes effect after instance_control suspend followed by start. Changing image, runtime.args, runtime.env, resources.memory, resources.vcpus or annotations, replacing or removing plugins or roms, and removing volumes take effect only after instance_control restart, or stop followed by start. An instance in standby keeps a snapshot of its memory, so those changes stay queued until a stop, which discards the snapshot."

const imageGetDescription = "Inspect images by reference, resolving each one from its registry, so public images such as nginx:latest work as well as your own. image_list only covers the images already in your account. Returns one JSON object per image."

type MCPCmd struct {
	ReadOnly    bool   `help:"Expose only the tools that cannot change anything: list, image_get and instance_logs."`
	DocsURL     string `name:"docs-url" env:"UNIKRAFT_DOCS_URL" default:"https://unikraft.com/docs/" placeholder:"url" help:"Documentation site served as resources. Pass an empty value to serve none."`
	Listen      string `name:"listen" placeholder:"host:port" help:"Serve the protocol over plain HTTP at this address, at /mcp, instead of stdio. Loopback addresses only unless --allow-remote is set."`
	AllowRemote bool   `name:"allow-remote" help:"Let --listen bind a non-loopback address. Every client then acts with this profile's credentials, so only do this behind an authenticating proxy or on a private network."`
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

		With %[1]s--listen host:port%[1]s the same tools are served over plain HTTP at %[1]s/mcp%[1]s,
		with a liveness check at %[1]s/healthz%[1]s, still using this profile's credentials.
		Only loopback addresses are accepted unless %[1]s--allow-remote%[1]s is given; when it
		is, put an authenticating proxy or the platform's TLS termination in front,
		since every client acts as you. A %[1]s--timeout%[1]s then stops accepting requests
		and drains the calls in flight. Clients register the server by URL:

		    {"mcpServers": {"unikraft": {"type": "http", "url": "http://127.0.0.1:8080/mcp"}}}
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
		{
			Description: "Serve over HTTP on the loopback interface",
			Commands: []string{
				"unikraft mcp --listen 127.0.0.1:8080",
			},
		},
		{
			Description: "Serve every interface, for a hosted deployment behind the platform's TLS",
			Commands: []string{
				"unikraft mcp --listen :8080 --allow-remote",
			},
		},
	}
}

func (c *MCPCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	loopback := true
	if c.Listen != "" {
		host, port, err := net.SplitHostPort(c.Listen)
		if err != nil {
			return fmt.Errorf("invalid listen address %q: %w", c.Listen, err)
		}
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return fmt.Errorf("invalid listen address %q: port must be a number from 0 to 65535", c.Listen)
		}
		// Loopback check: IP literals are tested directly, "localhost" is trusted
		// without resolving it, and any other hostname counts as remote.
		if ip, err := netip.ParseAddr(host); err == nil {
			loopback = ip.IsLoopback()
		} else {
			loopback = host == "localhost"
		}
		if !loopback && !c.AllowRemote {
			return fmt.Errorf("--listen %s binds a non-loopback address and the server has no authentication, so every client would act with this profile's credentials; pass --allow-remote to accept that", c.Listen)
		}
	}

	srv, err := newMCPServer(ctx, stdio.Stderr, partition, *c)
	if err != nil {
		return err
	}

	if c.Listen == "" {
		log.G(ctx).Info().Bool("read-only", c.ReadOnly).Msg("serving MCP over stdio")
		err = srv.Run(ctx)
	} else {
		if !loopback {
			log.G(ctx).Warn().Str("listen", c.Listen).Msg("serving MCP without authentication on a non-loopback address: every client acts with this profile's credentials")
		}
		log.G(ctx).Info().Bool("read-only", c.ReadOnly).Str("listen", c.Listen).Msg("serving MCP over HTTP")
		err = srv.ListenAndServe(ctx, c.Listen)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
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
		srv.AddTools[Instance](ctx, mcpserver.WithCreateDefault("autostart", true), mcpserver.WithEditNote(instanceEditNote)),
		srv.AddTools[InstanceTemplate](ctx),
		srv.AddTools[InstanceCheckpoint](ctx),
		srv.AddTools[Volume](ctx),
		srv.AddTools[VolumeTemplate](ctx),
		srv.AddTools[ServiceGroup](ctx),
		srv.AddListTool[Certificate](ctx),
		srv.AddCreateTool[Certificate](ctx),
		srv.AddDeleteTool[Certificate](ctx),
		srv.AddListTool[ImageEntry](ctx),
		srv.AddGetTool[Image](ctx, imageGetDescription),
		srv.AddDeleteTool[Image](ctx),
		srv.AddListTool[Metro](ctx),
		srv.AddListTool[Profile](ctx),
		srv.AddInstanceTools(mcpInstanceCommands{}),
	); err != nil {
		return nil, err
	}
	return srv, nil
}
