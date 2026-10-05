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

func connectHTTP(t *testing.T, ctx context.Context, endpoint string, client *http.Client) *mcp.ClientSession {
	t.Helper()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: client}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
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
	fake := &fakeInstances{}
	srv, err := mcpserver.New(ctx, mcpserver.Options{})
	require.NoError(t, err)
	require.NoError(t, srv.AddTools[modelt.TestModel](ctx))
	require.NoError(t, srv.AddInstanceTools(fake))
	ts := serveHTTP(t, ctx, srv)
	session := connectHTTP(t, ctx, ts.URL+mcpserver.HTTPPath, ts.Client())

	tools := listTools(t, ctx, session)
	assert.Contains(t, tools, "test_list")
	assert.Contains(t, tools, "instance_control")

	res := call(t, ctx, session, "test_list", map[string]any{"fields": []string{"name"}})
	require.False(t, res.IsError, text(res))
	assert.Equal(t, []map[string]any{{"name": "a"}}, resources(t, res))

	res = call(t, ctx, session, "instance_logs", map[string]any{"key": "web"})
	require.False(t, res.IsError, text(res))
	assert.Contains(t, text(res), "line 1")
	assert.Equal(t, []string{"logs web tail=200"}, fake.calls)
}

func TestHTTPEndpoints(t *testing.T) {
	ctx := t.Context()
	srv, err := mcpserver.New(ctx, mcpserver.Options{})
	require.NoError(t, err)
	ts := serveHTTP(t, ctx, srv)
	client := ts.Client()

	resp, err := client.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok\n", readBody(t, resp))

	resp, err = client.Get(ts.URL + mcpserver.HTTPPath)
	require.NoError(t, err)
	readBody(t, resp)
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Allow"), "POST")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+mcpserver.HTTPPath, strings.NewReader("hello"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err = client.Do(req)
	require.NoError(t, err)
	readBody(t, resp)
	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)

	resp, err = client.Get(ts.URL + "/nope")
	require.NoError(t, err)
	readBody(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	req, err = http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+mcpserver.HTTPPath, strings.NewReader("{}"))
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
	srv, err := mcpserver.New(ctx, mcpserver.Options{})
	require.NoError(t, err)
	require.NoError(t, srv.AddInstanceTools(&fakeInstances{}))

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ctx, l) }()

	session := connectHTTP(t, ctx, "http://"+l.Addr().String()+mcpserver.HTTPPath, http.DefaultClient)
	assert.Contains(t, listTools(t, ctx, session), "instance_logs")
	require.NoError(t, session.Close())

	cancel()
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not stop after the context was cancelled")
	}
}

func TestHTTPListenAndServeBadAddr(t *testing.T) {
	srv, err := mcpserver.New(t.Context(), mcpserver.Options{})
	require.NoError(t, err)
	require.Error(t, srv.ListenAndServe(t.Context(), "127.0.0.1:nope"))
}
