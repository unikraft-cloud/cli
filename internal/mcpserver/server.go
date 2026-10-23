// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/cli/internal/httpclient"
)

// Options configures a Server.
type Options struct {
	Name         string
	Title        string
	WebsiteURL   string
	Version      string
	Instructions string
	Logger       *slog.Logger

	DocsURL    string
	HTTPClient *http.Client
}

// Server serves an MCP server over stdio or HTTP, with a documentation site
// as resources.
type Server struct {
	srv  *mcp.Server
	opts Options
}

func New(ctx context.Context, opts Options) (*Server, error) {
	if opts.HTTPClient == nil {
		opts.HTTPClient = httpclient.GetClient(false)
	}
	var d *docs
	if opts.DocsURL != "" {
		var err error
		if d, err = newDocs(opts.DocsURL, opts.Name+"://docs", opts.Title, opts.HTTPClient); err != nil {
			return nil, err
		}
	}

	s := &Server{
		srv: mcp.NewServer(&mcp.Implementation{
			Name:       opts.Name,
			Title:      opts.Title,
			Version:    opts.Version,
			WebsiteURL: opts.WebsiteURL,
		}, &mcp.ServerOptions{
			Instructions: opts.Instructions,
			Logger:       opts.Logger,
		}),
		opts: opts,
	}
	if d != nil {
		s.addDocs(ctx, d)
	}
	return s, nil
}

func (s *Server) MCP() *mcp.Server {
	return s.srv
}

func (s *Server) Run(ctx context.Context) error {
	return s.srv.Run(ctx, &mcp.StdioTransport{})
}
