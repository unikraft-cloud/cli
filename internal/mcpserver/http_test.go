// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
	"unikraft.com/cli/internal/mcpserver"
	"unikraft.com/cli/internal/mcpserver/mcptest"
	modelmcp "unikraft.com/cli/internal/model/mcp"
	modelt "unikraft.com/cli/internal/model/testing"
)

func newServer(t *testing.T, opts mcpserver.Options) *mcpserver.Server {
	t.Helper()
	opts.Name, opts.Title = "unikraft", "Unikraft Cloud"
	srv, err := mcpserver.New(opts)
	require.NoError(t, err)
	return srv
}

func TestServe(t *testing.T) {
	t.Run("http", func(t *testing.T) {
		testServe(t, mcpserver.Options{}, "http", http.DefaultClient)
	})
	t.Run("https", func(t *testing.T) {
		cert := integ.GenerateCert(t, "localhost")
		dir := t.TempDir()
		opts := mcpserver.Options{TLSCertFile: filepath.Join(dir, "cert.pem"), TLSKeyFile: filepath.Join(dir, "key.pem")}
		require.NoError(t, os.WriteFile(opts.TLSCertFile, []byte(cert.Chain), 0o600))
		require.NoError(t, os.WriteFile(opts.TLSKeyFile, []byte(cert.Key), 0o600))
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
		testServe(t, opts, "https", client)
	})
}

func testServe(t *testing.T, opts mcpserver.Options, scheme string, client *http.Client) {
	env := modelt.NewTestEnv()
	env.Add(modelt.TestModel{ID: "1", Name: "a"})
	ctx, cancel := context.WithCancel(modelt.WithTestEnv(t.Context(), env))
	defer cancel()
	srv := newServer(t, opts)
	require.NoError(t, modelmcp.New(srv.MCP(), modelmcp.Options{}).AddTools[modelt.TestModel](ctx))

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ctx, l) }()
	url := scheme + "://" + l.Addr().String() + mcpserver.HTTPPath

	session := mcptest.Connect(t, &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: client})
	res := mcptest.CallTool(t, ctx, session, "test_list", map[string]any{"fields": []string{"name"}})
	require.False(t, res.IsError, mcptest.Text(res))
	assert.Equal(t, []map[string]any{{"name": "a"}}, mcptest.Resources(t, res))
	require.NoError(t, session.Close())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := client.Do(req)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	cancel()
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("server did not stop after the context was cancelled")
	}
}
