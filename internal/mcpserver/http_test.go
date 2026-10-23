// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/mcpserver"
	"unikraft.com/cli/internal/mcpserver/mcptest"
	modelt "unikraft.com/cli/internal/model/testing"
)

func serveHTTP(t *testing.T, ctx context.Context, srv *mcpserver.Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(srv.Handler())
	ts.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return string(body)
}

func TestHTTPTools(t *testing.T) {
	env := modelt.NewTestEnv()
	env.Add(modelt.TestModel{ID: "1", Name: "a"})
	ctx := modelt.WithTestEnv(t.Context(), env)
	ts := serveHTTP(t, ctx, newServer(t, ctx, mcpserver.Options{}, testTools(ctx)))
	session := mcptest.Connect(t, &mcp.StreamableClientTransport{Endpoint: ts.URL + mcpserver.HTTPPath, HTTPClient: ts.Client()})

	res := call(t, ctx, session, "test_list", map[string]any{"fields": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "a"}}, resources(t, res))
}

func TestHTTPEndpoints(t *testing.T) {
	ctx := t.Context()
	ts := serveHTTP(t, ctx, newServer(t, ctx, mcpserver.Options{}))
	client := ts.Client()

	resp, err := client.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok\n", readBody(t, resp))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+mcpserver.HTTPPath, strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err = client.Do(req)
	require.NoError(t, err)
	readBody(t, resp)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestHTTPServeShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	srv := newServer(t, ctx, mcpserver.Options{})

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ctx, l) }()

	session := mcptest.Connect(t, &mcp.StreamableClientTransport{Endpoint: "http://" + l.Addr().String() + mcpserver.HTTPPath, HTTPClient: http.DefaultClient})
	require.NoError(t, session.Close())

	cancel()
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not stop after the context was cancelled")
	}
}
