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
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/x/log"
)

const (
	docsURI      = "unikraft://docs"
	docsMIMEType = "text/markdown"
	docsIndex    = "llms.txt"
	docsCacheTTL = 10 * time.Minute
	docsMaxBytes = 8 << 20
)

var (
	errDocsNotFound = errors.New("documentation page not found")

	// docsListed holds the path prefixes registered as individual resources;
	// every other page stays readable through the index and the template.
	docsListed = []string{"features/", "platform/", "use-cases/"}
)

type docs struct {
	base   *url.URL
	client *http.Client
	entry  *regexp.Regexp
	link   *regexp.Regexp

	mu    sync.Mutex
	cache map[string]cachedPage
}

type cachedPage struct {
	body    string
	fetched time.Time
}

type docsEntry struct {
	path, title, description string
}

func newDocs(baseURL string, client *http.Client) (*docs, error) {
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
	prefix := regexp.QuoteMeta(base.Path)
	return &docs{
		base:   base,
		client: client,
		entry:  regexp.MustCompile(`^- \[([^\]]+)\]\(` + prefix + `([^)\s]+?)\.md\)(?::\s*(.*))?$`),
		link:   regexp.MustCompile(`\]\(` + prefix + `([^)\s]+?)\.md\)`),
		cache:  make(map[string]cachedPage),
	}, nil
}

func (s *Server) addDocs(ctx context.Context, d *docs) {
	site := d.base.String()
	s.srv.AddResource(&mcp.Resource{
		URI:         docsURI,
		Name:        "docs",
		Title:       "Unikraft Cloud documentation",
		Description: "Table of contents of " + site + ". Every entry is a readable " + docsURI + "/<path> resource.",
		MIMEType:    docsMIMEType,
	}, d.read)
	s.srv.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: docsURI + "/{+path}",
		Name:        "docs-page",
		Title:       "Unikraft Cloud documentation page",
		Description: "A documentation page by path, such as " + docsURI + "/cli/fields, with the same content as " + site + "<path>.",
		MIMEType:    docsMIMEType,
	}, d.read)
	go s.addDocsPages(ctx, d)
}

func (s *Server) addDocsPages(ctx context.Context, d *docs) {
	index, _, err := d.fetch(ctx, docsIndex)
	if err != nil {
		log.G(ctx).Warn().Err(err).Msg("documentation index unavailable, pages can still be read by path")
		return
	}
	for _, entry := range d.entries(index) {
		if !slices.ContainsFunc(docsListed, func(prefix string) bool { return strings.HasPrefix(entry.path, prefix) }) {
			continue
		}
		s.srv.AddResource(&mcp.Resource{
			URI:         docsURI + "/" + entry.path,
			Name:        entry.path,
			Title:       entry.title,
			Description: entry.description,
			MIMEType:    docsMIMEType,
		}, d.read)
	}
}

func (d *docs) entries(index string) []docsEntry {
	var entries []docsEntry
	for line := range strings.SplitSeq(index, "\n") {
		m := d.entry.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || !validDocsPath(m[2]) {
			continue
		}
		entries = append(entries, docsEntry{path: m[2], title: m[1], description: m[3]})
	}
	return entries
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
	if uri == docsURI {
		index, cached, err := d.fetch(ctx, docsIndex)
		if err != nil {
			return "", cached, err
		}
		return d.link.ReplaceAllString(index, "]("+docsURI+"/$1)"), cached, nil
	}
	path, ok := strings.CutPrefix(uri, docsURI+"/")
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
		return "", false, fmt.Errorf("fetching %s: %s", target, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, docsMaxBytes))
	if err != nil {
		return "", false, fmt.Errorf("reading %s: %w", target, err)
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
