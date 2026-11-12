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
	"slices"
	"strings"

	"github.com/MakeNowJust/heredoc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/version"

	"unikraft.com/cli/internal/branding"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mcp"
	"unikraft.com/cli/pkg/mcpserver"
	"unikraft.com/cli/pkg/model"
	modelmcp "unikraft.com/cli/pkg/model/mcp"
	xstdio "unikraft.com/x/stdio"
)

const instanceEditNote = "Edits to tags, delete-lock, autokill, scale-to-zero and sched-priority apply at once, even while the instance runs. Other edits to a running instance are queued until it next stops. Adding plugins, roms or volumes takes effect after instance_control suspend followed by start. Changing image, runtime.args, runtime.env, resources.memory, resources.vcpus or annotations, replacing or removing plugins or roms, and removing volumes take effect only after instance_control restart, or stop followed by start. An instance in standby keeps a snapshot of its memory, so those changes stay queued until a stop, which discards the snapshot."

var mcpResourceTypes = []string{
	"certificate",
	"image",
	"instance",
	"instance_checkpoint",
	"instance_template",
	"metro",
	"profile",
	"sandbox",
	"service",
	"volume",
	"volume_template",
}

const imageGetDescription = "Inspect images by reference, resolving each one from its registry, so public images such as nginx:latest work as well as your own. image_list only covers the images already in your account. Returns one JSON object per image."

type MCPCmd struct {
	ReadOnly  bool     `help:"Expose only the tools that cannot change anything: list, image_get, instance_logs and sandbox_read."`
	Resources []string `name:"resources" placeholder:"type,..." help:"Serve only the tools of these resource types. The names are the prefixes of the tool names. Defaults to every type."`
	DocsURL   string   `name:"docs-url" env:"UNIKRAFT_DOCS_URL" default:"${docs_url}" placeholder:"url" hidden:"" help:"Documentation site served as resources. Pass an empty value to serve none."`

	mcpserver.Flags
}

func (MCPCmd) Help() string {
	return heredoc.Docf(`
		The server speaks the Model Context Protocol over stdin and stdout, or over
		HTTP with %[1]s--listen%[1]s, so an AI assistant such as Claude Code, Claude Desktop
		or Cursor can manage Unikraft Cloud through typed tools instead of shelling
		out to the CLI.

		Every resource type gets list, create, edit and delete tools, named like
		%[1]sinstance_list%[1]s or %[1]svolume_create%[1]s. A list tool also fetches specific resources
		when given keys. Instances additionally get %[1]sinstance_control%[1]s for start,
		stop, restart and suspend, and %[1]sinstance_logs%[1]s. Sandboxes get %[1]ssandbox_exec%[1]s,
		%[1]ssandbox_read%[1]s and %[1]ssandbox_write%[1]s for the commands and files inside them.
		The tools use the current profile and fan out across its metros exactly like
		the matching CLI commands do. Every tool call reads the configuration file
		again, so %[1]sunikraft login%[1]s and %[1]sunikraft profile use%[1]s take effect without
		restarting the server. Unless %[1]s--profile%[1]s pins one, %[1]sprofile_use%[1]s lists the
		profiles and switches between them.

		Pass %[1]s--resources%[1]s with a comma-separated list of resource types to serve
		only their tools, such as %[1]s--resources instance,volume,metro%[1]s. The names
		are the prefixes of the tool names. A shorter tool list costs an assistant
		less context on every turn.

		The documentation is served as resources: %[1]sunikraft://docs%[1]s is the table of
		contents and %[1]sunikraft://docs/<path>%[1]s a page, such as %[1]sunikraft://docs/cli/fields%[1]s.
		Pages are fetched from the documentation site when read, so they cost
		nothing until an assistant asks for one.

		Diagnostics always go to stderr, because stdout carries the protocol in stdio
		mode. Raise them with %[1]s--log-level debug%[1]s. A %[1]s--timeout%[1]s ends the whole
		session when it expires.

		Register the server with a client as a stdio server, for example:

		    {"mcpServers": {"unikraft": {"command": "unikraft", "args": ["mcp"]}}}

		With %[1]s--listen host:port%[1]s the same tools are served over HTTP at %[1]s/mcp%[1]s, with
		a liveness check at %[1]s/healthz%[1]s, still using this profile's credentials. Pass
		%[1]s--tls-cert%[1]s and %[1]s--tls-key%[1]s to serve HTTPS instead. Only loopback addresses
		are accepted unless %[1]s--allow-remote%[1]s is given. TLS encrypts the traffic but
		does not authenticate clients, so then put an authenticating proxy in front,
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
			Description: "Serve only the instance, volume and metro tools",
			Commands: []string{
				"unikraft mcp --resources instance,volume,metro",
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
			Description: "Serve HTTPS on every interface with a certificate",
			Commands: []string{
				"unikraft mcp --listen :8443 --allow-remote --tls-cert cert.pem --tls-key key.pem",
			},
		},
		{
			Description: "Serve every interface on an instance whose service terminates TLS",
			Commands: []string{
				"unikraft mcp --listen :8080 --allow-remote",
			},
		},
	}
}

func (c *MCPCmd) Run(ctx context.Context, stdio xstdio.Stdio, partition *model.Partition) error {
	if err := c.Check(); err != nil {
		return err
	}
	srv, err := newMCPServer(ctx, stdio.Stderr, partition, *c)
	if err != nil {
		return err
	}
	log.G(ctx).Debug().Bool("read-only", c.ReadOnly).Msg("starting MCP server")
	return c.Serve(ctx, srv)
}

func newMCPServer(ctx context.Context, stderr io.Writer, partition *model.Partition, c MCPCmd) (*mcpserver.Server, error) {
	if err := checkResources(c.Resources); err != nil {
		return nil, err
	}
	var logger *slog.Logger
	if log.G(ctx).GetLevel() <= log.DebugLevel {
		logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	srv, err := c.Server(mcpserver.Options{
		Name:         branding.Name,
		Title:        branding.Product,
		WebsiteURL:   branding.WebsiteURL,
		Version:      version.Version,
		Instructions: mcp.Instructions(c.ReadOnly, c.Resources, c.DocsURL, mcpserver.DocsURI(branding.Name)),
		Logger:       logger,
		DocsURL:      c.DocsURL,
	})
	if err != nil {
		return nil, err
	}
	srv.MCP().AddReceivingMiddleware(reloadConfig)
	tools := modelmcp.New(srv.MCP(), modelmcp.Options{
		Partition: partition,
		ReadOnly:  c.ReadOnly,
		Resources: c.Resources,
		Stderr:    stderr,
	})
	if err := errors.Join(
		tools.AddListTool[Instance](ctx),
		tools.AddCreateTool[Instance](ctx,
			modelmcp.WithCreateDefault("autostart", true),
			modelmcp.WithCreateExample("image", "metro", "service.services", "resources.memory"),
		),
		tools.AddEditTool[Instance](ctx, modelmcp.WithEditNote(instanceEditNote)),
		tools.AddDeleteTool[Instance](ctx),
		tools.AddListTool[InstanceTemplate](ctx),
		tools.AddCreateTool[InstanceTemplate](ctx),
		tools.AddEditTool[InstanceTemplate](ctx),
		tools.AddDeleteTool[InstanceTemplate](ctx),
		tools.AddListTool[InstanceCheckpoint](ctx),
		tools.AddCreateTool[InstanceCheckpoint](ctx),
		tools.AddEditTool[InstanceCheckpoint](ctx),
		tools.AddDeleteTool[InstanceCheckpoint](ctx),
		tools.AddListTool[Volume](ctx),
		tools.AddCreateTool[Volume](ctx, modelmcp.WithCreateExample("metro", "size", "access-mode", "filesystem")),
		tools.AddEditTool[Volume](ctx),
		tools.AddDeleteTool[Volume](ctx),
		tools.AddListTool[VolumeTemplate](ctx),
		tools.AddCreateTool[VolumeTemplate](ctx),
		tools.AddEditTool[VolumeTemplate](ctx),
		tools.AddDeleteTool[VolumeTemplate](ctx),
		tools.AddListTool[ServiceGroup](ctx),
		tools.AddCreateTool[ServiceGroup](ctx, modelmcp.WithCreateExample("metro", "domains", "autokill")),
		tools.AddEditTool[ServiceGroup](ctx),
		tools.AddDeleteTool[ServiceGroup](ctx),
		tools.AddListTool[Certificate](ctx),
		tools.AddCreateTool[Certificate](ctx),
		tools.AddEditTool[Certificate](ctx),
		tools.AddDeleteTool[Certificate](ctx),
		tools.AddListTool[ImageEntry](ctx),
		tools.AddGetTool[Image](ctx, imageGetDescription),
		tools.AddDeleteTool[Image](ctx),
		tools.AddListTool[Metro](ctx),
		addProfileUseTool(ctx, tools, partition),
		mcp.AddInstanceTools(tools, mcpInstanceCommands{}),
		mcp.AddSandboxTools(tools, mcpSandboxCommands{partition: partition}),
	); err != nil {
		return nil, err
	}
	return srv, nil
}

func reloadConfig(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
		if _, ok := req.(*sdk.CallToolRequest); ok {
			started := config.G(ctx)
			profile, _ := started.OverriddenCurrentProfile()
			cfg, err := loadConfig(started.Path, profile)
			if err != nil {
				var res sdk.CallToolResult
				res.SetError(err)
				return &res, nil
			}
			ctx = config.WithConfig(ctx, cfg)
		}
		return next(ctx, method, req)
	}
}

func checkResources(names []string) error {
	var unknown []string
	for _, name := range names {
		if !slices.Contains(mcpResourceTypes, modelmcp.NormalizeName(name)) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return fmt.Errorf("--resources: unknown resource types: %s; known: %s",
		strings.Join(unknown, ", "), strings.Join(mcpResourceTypes, ", "))
}
