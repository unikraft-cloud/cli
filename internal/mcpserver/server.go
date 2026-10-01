// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/resource"
)

// Options configures a Server.
type Options struct {
	Partition *resource.Partition
	ReadOnly  bool
	Version   string
	Stderr    io.Writer
	Logger    *slog.Logger

	DocsURL    string
	HTTPClient *http.Client
}

// Server exposes the CLI's resources to MCP clients as tools, and the
// documentation as resources.
type Server struct {
	srv  *mcp.Server
	opts Options
}

func New(ctx context.Context, opts Options) (*Server, error) {
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = httpclient.GetClient(false)
	}
	var d *docs
	if opts.DocsURL != "" {
		var err error
		if d, err = newDocs(opts.DocsURL, opts.HTTPClient); err != nil {
			return nil, err
		}
	}

	s := &Server{
		srv: mcp.NewServer(&mcp.Implementation{
			Name:       "unikraft",
			Title:      "Unikraft Cloud",
			Version:    opts.Version,
			WebsiteURL: "https://unikraft.cloud",
		}, &mcp.ServerOptions{
			Instructions: instructions(opts.ReadOnly, d),
			Logger:       opts.Logger,
		}),
		opts: opts,
	}
	if d != nil {
		s.addDocs(ctx, d)
	}
	return s, nil
}

func (s *Server) MCP() *mcp.Server { return s.srv }

func (s *Server) Run(ctx context.Context) error {
	return s.srv.Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) Connect(ctx context.Context, t mcp.Transport) (*mcp.ServerSession, error) {
	return s.srv.Connect(ctx, t, nil)
}

func instructions(readOnly bool, d *docs) string {
	text := `Tools are named <resource>_<operation>: list, create, edit and delete for each Unikraft Cloud resource type. A list tool returns everything unless keys names specific resources, so it doubles as get. image_get inspects any image reference, including public ones. instance_control starts, stops, restarts or suspends instances and instance_logs reads their console output.

Resource keys accept a name, a UUID, or metro/name to target one metro. Reads fan out to every metro of the current profile; narrow a list with filter entries such as metro==fra or state==running.

Create and edit take set, add and del objects keyed by dotted field paths, exactly like the CLI's --set, --add and --del flags. Values may be strings, parsed as on the command line, or JSON values of the field's type. Pass dry_run to preview the resulting patch without applying it.

Results come back as JSON text and as structured content under "resources". Pass fields to limit which field paths are returned.`
	if d != nil {
		text += "\n\nThe documentation is available as resources: read " + docsURI + " for the table of contents and " + docsURI + "/<path> for a page, the same content as " + d.base.String() + "<path>. The CLI reference lives under " + docsURI + "/cli/unikraft."
	}
	if readOnly {
		text += "\n\nThis server runs in read-only mode: only the list, image_get and instance_logs tools are available."
	}
	return text
}
