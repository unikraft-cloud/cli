// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"

	"unikraft.com/x/log"
)

// Flags choose how a command serves MCP: over stdio, or HTTP with --listen.
type Flags struct {
	Listen      string `name:"listen" placeholder:"host:port" help:"Serve the protocol over HTTP at this address, at /mcp, instead of stdio. Loopback addresses only unless --allow-remote is set."`
	AllowRemote bool   `name:"allow-remote" help:"Let --listen bind a non-loopback address. Every client then acts with the server's credentials, so only do this behind an authenticating proxy or on a private network."`
	TLSCert     string `name:"tls-cert" and:"tls" type:"path" placeholder:"file" help:"Serve --listen over HTTPS with this PEM certificate chain."`
	TLSKey      string `name:"tls-key" and:"tls" type:"path" placeholder:"file" help:"PEM private key of --tls-cert."`
}

// Check rejects a --listen that isn't a valid address, or isn't loopback
// without --allow-remote.
func (f Flags) Check() error {
	if f.Listen == "" {
		return nil
	}
	_, port, err := net.SplitHostPort(f.Listen)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", f.Listen, err)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("invalid listen address %q: port must be a number from 0 to 65535", f.Listen)
	}
	if !f.loopback() && !f.AllowRemote {
		return fmt.Errorf("--listen %s binds a non-loopback address and the server has no authentication, so every client would act with the server's credentials; pass --allow-remote to accept that", f.Listen)
	}
	return nil
}

// Server builds a Server with the TLS files of the flags.
func (f Flags) Server(opts Options) (*Server, error) {
	opts.TLSCertFile = f.TLSCert
	opts.TLSKeyFile = f.TLSKey
	return New(opts)
}

// Serve serves srv until ctx is cancelled.
func (f Flags) Serve(ctx context.Context, srv *Server) error {
	var err error
	if f.Listen == "" {
		log.G(ctx).Info().Msg("serving MCP over stdio")
		err = srv.Run(ctx)
	} else {
		if !f.loopback() {
			log.G(ctx).Warn().Str("listen", f.Listen).Msg("serving MCP without authentication on a non-loopback address: every client acts with the server's credentials")
		}
		log.G(ctx).Info().Str("listen", f.Listen).Bool("tls", f.TLSCert != "").Msg("serving MCP over HTTP")
		err = srv.ListenAndServe(ctx, f.Listen)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (f Flags) loopback() bool {
	host, _, err := net.SplitHostPort(f.Listen)
	if err != nil {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}
