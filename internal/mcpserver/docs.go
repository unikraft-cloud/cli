// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/x/log"
)

const (
	docsMIMEType = "text/markdown"
	docsIndex    = "llms.txt"
	docsCacheTTL = 10 * time.Minute
	docsMaxBytes = 8 << 20
)

var errDocsNotFound = errors.New("documentation page not found")

type docs struct {
	base   *url.URL
	uri    string
	title  string
	client *http.Client

	mu    sync.Mutex
	cache map[string]cachedPage
}

type cachedPage struct {
	body    string
	fetched time.Time
}

func newDocs(baseURL, uri, title string, client *http.Client) (*docs, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid documentation URL %q: %w", baseURL, err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid documentation URL %q: scheme and host are required", baseURL)
	}
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	return &docs{
		base:   base,
		uri:    uri,
		title:  title,
		client: client,
		cache:  make(map[string]cachedPage),
	}, nil
}

func (s *Server) addDocs(d *docs) {
	site := d.base.String()
	s.srv.AddResource(&mcp.Resource{
		URI:         d.uri,
		Name:        "docs",
		Title:       d.title + " documentation",
		Description: "Table of contents of " + site + ". Every entry is a readable " + d.uri + "/<path> resource.",
		MIMEType:    docsMIMEType,
	}, d.read)
	s.srv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: d.uri + "/{+path}",
		Name:        "docs-page",
		Title:       d.title + " documentation page",
		Description: "A documentation page by path, such as " + d.uri + "/cli/fields, with the same content as " + site + "<path>.",
		MIMEType:    docsMIMEType,
	}, d.read)
}

func (d *docs) read(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := req.Params.URI
	start := time.Now()
	body, cached, err := d.lookup(ctx, uri)
	log.G(ctx).Debug().
		Str("uri", uri).
		Bool("cached", cached).
		Dur("took", time.Since(start)).
		Err(err).
		Msg("resource read")
	if errors.Is(err, errDocsNotFound) {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: docsMIMEType, Text: body}},
	}, nil
}

func (d *docs) lookup(ctx context.Context, uri string) (string, bool, error) {
	if uri == d.uri {
		index, cached, err := d.fetch(ctx, docsIndex)
		if err != nil {
			return "", cached, err
		}
		return strings.ReplaceAll(index, "]("+d.base.Path, "]("+d.uri+"/"), cached, nil
	}
	path, ok := strings.CutPrefix(uri, d.uri+"/")
	path = strings.TrimSuffix(path, ".md")
	if !ok || !validDocsPath(path) {
		return "", false, errDocsNotFound
	}
	return d.fetch(ctx, path+".md")
}

func (d *docs) fetch(ctx context.Context, rel string) (string, bool, error) {
	d.mu.Lock()
	cached, ok := d.cache[rel]
	d.mu.Unlock()
	if ok && time.Since(cached.fetched) < docsCacheTTL {
		return cached.body, true, nil
	}

	target := d.base.JoinPath(rel).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", docsMIMEType)
	resp, err := d.client.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("fetching %s: %w", target, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", false, fmt.Errorf("%w: %s", errDocsNotFound, target)
	case resp.StatusCode != http.StatusOK:
		return "", false, fmt.Errorf("documentation site returned %s for %s", resp.Status, target)
	}
	if mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mediaType != docsMIMEType && mediaType != "text/plain" {
		return "", false, fmt.Errorf("documentation site returned %q instead of markdown for %s", mediaType, target)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, docsMaxBytes+1))
	if err != nil {
		return "", false, fmt.Errorf("reading %s: %w", target, err)
	}
	if len(data) > docsMaxBytes {
		return "", false, fmt.Errorf("%s is larger than %d MiB", target, docsMaxBytes>>20)
	}

	body := string(data)
	d.mu.Lock()
	d.cache[rel] = cachedPage{body: body, fetched: time.Now()}
	d.mu.Unlock()
	return body, false, nil
}

func validDocsPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") {
		return false
	}
	for segment := range strings.SplitSeq(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
