// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDemoteHeadings(t *testing.T) {
	for _, test := range []struct {
		name     string
		text     string
		minLevel int
		want     string
	}{
		{
			name:     "headings move down together",
			text:     "## Section\n\nBody.\n\n### Nested",
			minLevel: 3,
			want:     "### Section\n\nBody.\n\n#### Nested",
		},
		{
			name:     "a description written at level one moves the whole way",
			text:     "# Overview\n\n## Detail",
			minLevel: 3,
			want:     "### Overview\n\n#### Detail",
		},
		{
			name:     "headings which are deep enough stay put",
			text:     "### A\n\n#### B",
			minLevel: 3,
			want:     "### A\n\n#### B",
		},
		{
			name:     "the sixth level is the deepest",
			text:     "##### A\n\n###### B",
			minLevel: 6,
			want:     "###### A\n\n###### B",
		},
		{
			name:     "text without headings is unchanged",
			text:     "Just prose.",
			minLevel: 3,
			want:     "Just prose.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, demoteHeadings(test.text, test.minLevel))
		})
	}
}

// A hash inside a code block is content, not a heading.
func TestDemoteHeadingsSkipsCode(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
	}{
		{"backtick fence", "```\n# not a heading\n```"},
		{"fence with an info string", "```go\n# not a heading\n```"},
		{"tilde fence", "~~~\n# not a heading\n~~~"},
		{"fence holding backticks", "````\n```\n# not a heading\n```\n````"},
		{"unclosed fence", "```\n# not a heading"},
		{"indented code block", "text\n\n    # not a heading\n    more code"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.text, demoteHeadings(test.text, 3))
		})
	}
}

func TestDemoteHeadingsKeepsTheLine(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		want string
	}{
		{
			name: "the indent of a heading in a list item",
			text: "- item\n\n  ## Sub",
			want: "- item\n\n  ### Sub",
		},
		{
			name: "up to three spaces of indent still make a heading",
			text: "   ## Indented",
			want: "   ### Indented",
		},
		{
			name: "a carriage return at the end of the line",
			text: "## A\r\nbody\r\n",
			want: "### A\r\nbody\r\n",
		},
		{
			name: "the text after the hashes",
			text: "## A  trailing  space  ",
			want: "### A  trailing  space  ",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, demoteHeadings(test.text, 3))
		})
	}
}

func TestHeadingLevel(t *testing.T) {
	for _, test := range []struct {
		name string
		line string
		want int
	}{
		{"one hash", "# Title", 1},
		{"six hashes", "###### Title", 6},
		{"seven hashes is not a heading", "####### Title", 0},
		{"a tab separates the hashes from the text", "#\tTitle", 1},
		{"hashes alone are an empty heading", "###", 3},
		{"a hash without a separator is not a heading", "#hashtag", 0},
		{"three spaces of indent are allowed", "   # Title", 1},
		{"four spaces of indent make a code block", "    # Title", 0},
		{"prose is not a heading", "Just prose.", 0},
		{"an empty line is not a heading", "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, headingLevel(test.line))
		})
	}
}
