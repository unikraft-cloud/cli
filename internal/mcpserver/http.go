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
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/x/log"
)

// HTTPPath is where the protocol is served in network mode.
const HTTPPath = "/mcp"

const (
	healthPath        = "/healthz"
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// Handler serves the protocol at HTTPPath over the streamable HTTP transport
// and a liveness check at /healthz. Every request runs on a temporary
// session, so any process can answer any request. Tool calls take their
// config and logger from the request context, which must therefore derive
// from the CLI context, as Serve arranges.
func (s *Server) Handler() http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.srv }, &mcp.StreamableHTTPOptions{
		Stateless: true,
		Logger:    s.opts.Logger,
	})

	mux := http.NewServeMux()
	mux.Handle(HTTPPath, streamable)
	mux.HandleFunc("GET "+healthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	})
	return http.NewCrossOriginProtection().Handler(mux)
}

// Serve serves Handler on l until ctx is cancelled, then stops accepting and
// drains in-flight calls for up to shutdownTimeout. It returns nil after a
// clean shutdown.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			log.G(ctx).Warn().Err(err).Msg("forcing http server close")
			_ = srv.Close()
		}
	})

	log.G(ctx).Info().Str("addr", l.Addr().String()).Msg("listening")
	var err error
	if s.opts.TLSCertFile != "" {
		err = srv.ServeTLS(l, s.opts.TLSCertFile, s.opts.TLSKeyFile)
	} else {
		err = srv.Serve(l)
	}
	if stop() {
		return err
	}
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, l)
}
