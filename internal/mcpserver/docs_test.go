// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/mcpserver"
)

const docsIndex = `# Documentation

> Documentation files for Large Language Models

## Documentation

- [Introduction](/docs/introduction.md)
- [Fields](/docs/cli/fields.md): Display, sort, filter, and edit resource fields
- [Instances](/docs/platform/instances.md): Run unikernels
- [Autoscale](/docs/features/autoscale.md)
- [NGINX](/docs/guides/nginx.md): Deploy NGINX
- [unikraft instances](/docs/cli/unikraft/instances.md): Manage instances.
- [Escaped](/docs/../etc/passwd.md)
- not a link
`

func newDocsSite(t *testing.T, indexStatus int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var pageRequests atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/markdown" {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		switch r.URL.Path {
		case "/docs/llms.txt":
			w.WriteHeader(indexStatus)
			fmt.Fprint(w, docsIndex)
		case "/docs/cli/fields.md":
			pageRequests.Add(1)
			fmt.Fprint(w, "# Fields\n\nBody.\n")
		case "/docs/introduction.md":
			fmt.Fprint(w, "# Introduction\n")
		case "/docs/guides/nginx.md":
			fmt.Fprint(w, "# NGINX\n")
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "# Not found\n")
		}
	}))
	t.Cleanup(site.Close)
	return site, &pageRequests
}

func TestDocsResources(t *testing.T) {
	site, pageRequests := newDocsSite(t, http.StatusOK)
	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.DebugLevel)
	ctx := log.WithLogger(t.Context(), &logger)
	srv, err := mcpserver.New(ctx, mcpserver.Options{DocsURL: site.URL + "/docs", HTTPClient: site.Client()})
	require.NoError(t, err)
	session := connect(t, ctx, srv)

	templates, err := session.ListResourceTemplates(ctx, nil)
	require.NoError(t, err)
	require.Len(t, templates.ResourceTemplates, 1)
	assert.Equal(t, "unikraft://docs/{+path}", templates.ResourceTemplates[0].URITemplate)

	byURI := map[string]*mcp.Resource{}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := session.ListResources(ctx, nil)
		require.NoError(c, err)
		assert.Len(c, res.Resources, 3)
		for _, r := range res.Resources {
			byURI[r.URI] = r
		}
	}, 5*time.Second, 20*time.Millisecond)
	assert.Contains(t, byURI, "unikraft://docs")
	assert.Contains(t, byURI, "unikraft://docs/features/autoscale")
	for _, unlisted := range []string{"introduction", "cli/fields", "guides/nginx", "cli/unikraft/instances"} {
		assert.NotContains(t, byURI, "unikraft://docs/"+unlisted)
	}
	instances := byURI["unikraft://docs/platform/instances"]
	require.NotNil(t, instances)
	assert.Equal(t, "platform/instances", instances.Name)
	assert.Equal(t, "Instances", instances.Title)
	assert.Equal(t, "Run unikernels", instances.Description)
	assert.Equal(t, "text/markdown", instances.MIMEType)

	page, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "unikraft://docs/cli/fields"})
	require.NoError(t, err)
	require.Len(t, page.Contents, 1)
	assert.Equal(t, "unikraft://docs/cli/fields", page.Contents[0].URI)
	assert.Equal(t, "text/markdown", page.Contents[0].MIMEType)
	assert.Equal(t, "# Fields\n\nBody.\n", page.Contents[0].Text)

	_, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "unikraft://docs/cli/fields"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), pageRequests.Load())
	assert.Contains(t, logs.String(), `"uri":"unikraft://docs/cli/fields","cached":false,"took":`)
	assert.Contains(t, logs.String(), `"uri":"unikraft://docs/cli/fields","cached":true,"took":`)

	toc, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "unikraft://docs"})
	require.NoError(t, err)
	require.Len(t, toc.Contents, 1)
	assert.Contains(t, toc.Contents[0].Text, "[Fields](unikraft://docs/cli/fields): Display")
	assert.Contains(t, toc.Contents[0].Text, "[Introduction](unikraft://docs/introduction)")
	assert.Contains(t, toc.Contents[0].Text, "[NGINX](unikraft://docs/guides/nginx): Deploy NGINX")

	guide, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "unikraft://docs/guides/nginx"})
	require.NoError(t, err)
	assert.Equal(t, "# NGINX\n", guide.Contents[0].Text)
	assert.NotContains(t, toc.Contents[0].Text, ".md)")

	_, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "unikraft://docs/missing/page"})
	require.ErrorContains(t, err, "not found")

	_, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "unikraft://docs/../etc/passwd"})
	require.ErrorContains(t, err, "not found")
}

func TestDocsIndexUnavailable(t *testing.T) {
	site, _ := newDocsSite(t, http.StatusInternalServerError)
	ctx := t.Context()
	srv, err := mcpserver.New(ctx, mcpserver.Options{DocsURL: site.URL + "/docs/", HTTPClient: site.Client()})
	require.NoError(t, err)
	session := connect(t, ctx, srv)

	page, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "unikraft://docs/introduction"})
	require.NoError(t, err)
	assert.Equal(t, "# Introduction\n", page.Contents[0].Text)

	_, err = session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "unikraft://docs"})
	require.ErrorContains(t, err, "500")
}

func TestDocsDisabledAndInvalid(t *testing.T) {
	ctx := t.Context()
	srv, err := mcpserver.New(ctx, mcpserver.Options{})
	require.NoError(t, err)
	session := connect(t, ctx, srv)
	res, err := session.ListResources(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, res.Resources)

	_, err = mcpserver.New(ctx, mcpserver.Options{DocsURL: "unikraft.com/docs"})
	require.ErrorContains(t, err, "invalid documentation URL")
}
