// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/alecthomas/kong"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/golden"
)

// docsNode builds the real command tree and returns the docsCommand node,
// with the colour profile the generators run under.
func docsNode(t *testing.T) *kong.Node {
	t.Helper()

	compat.Profile = colorprofile.NoTTY
	lipgloss.Writer.Profile = colorprofile.NoTTY
	t.Setenv("NO_COLOR", "1")

	parser, err := CreateParser()
	require.NoError(t, err)

	for node := range IterChildren(parser.Model.Node) {
		if NodePath(node) == docsCommand {
			return node
		}
	}
	t.Fatalf("command %q not found", docsCommand)
	return nil
}

// The api command is the one whose description is written as Markdown, so it
// exercises headings, code blocks and escapes through both generators.
const docsCommand = "unikraft api"

func TestGenerateMarkdownGolden(t *testing.T) {
	node := docsNode(t)

	dir := t.TempDir()
	require.NoError(t, generateMarkdown(context.Background(), node, dir))

	out, err := os.ReadFile(filepath.Join(dir, "unikraft", "api.mdx"))
	require.NoError(t, err)

	golden.Assert(t, string(out), "api.mdx.golden")
}

func TestGenerateManGolden(t *testing.T) {
	node := docsNode(t)

	date := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	header := &ManHeader{
		Title:   "Unikraft CLI",
		Section: "1",
		Date:    &date,
		Source:  "Unikraft Documentation",
		Manual:  "Unikraft Manual",
	}

	out, err := genManContent(node, header)
	require.NoError(t, err)

	golden.Assert(t, string(out), "api.man.golden")
}

// A heading of the description has to become a subsection of DESCRIPTION.
// md2man renders both "#" and "##" as a section, which would put it beside
// DESCRIPTION instead.
func TestGenerateManDemotesDescriptionHeadings(t *testing.T) {
	node := docsNode(t)

	date := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	out, err := genManContent(node, &ManHeader{Section: "1", Date: &date})
	require.NoError(t, err)

	body := string(out)
	require.Contains(t, body, "### Request body syntax")
	require.NotContains(t, body, "\n## Request body syntax")
}

// Nothing the generators write may carry terminal styling.
func TestGeneratedDocsCarryNoAnsi(t *testing.T) {
	node := docsNode(t)

	dir := t.TempDir()
	require.NoError(t, generateMarkdown(context.Background(), node, dir))
	mdx, err := os.ReadFile(filepath.Join(dir, "unikraft", "api.mdx"))
	require.NoError(t, err)

	date := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	man, err := genManContent(node, &ManHeader{Section: "1", Date: &date})
	require.NoError(t, err)

	for name, out := range map[string]string{"mdx": string(mdx), "man": string(man)} {
		require.Equal(t, out, ansi.Strip(out), "%s output carries ANSI escapes", name)
		require.NotContains(t, out, "\x1b", "%s output carries an escape byte", name)
	}
}
