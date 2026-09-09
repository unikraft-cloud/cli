// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "testing"

func TestFormatDetail(t *testing.T) {
	for _, test := range []struct {
		name string
		in   string
		want string
	}{
		{
			name: "prose is left alone",
			in:   "The API command makes direct HTTP requests.\nIt is useful for scripting.",
			want: "The API command makes direct HTTP requests.\nIt is useful for scripting.",
		},
		{
			name: "all-caps heading becomes a section",
			in:   "Intro.\n\nREQUEST BODY SYNTAX\n\nMore prose.",
			want: "Intro.\n\n## Request body syntax\n\nMore prose.",
		},
		{
			name: "acronyms keep their case",
			in:   "NESTED JSON SYNTAX\n\nBody.",
			want: "## Nested JSON syntax\n\nBody.",
		},
		{
			name: "hyphenated heading",
			in:   "TOP-LEVEL ARRAY SYNTAX\n\nBody.",
			want: "## Top-level array syntax\n\nBody.",
		},
		{
			name: "all-caps line inside a paragraph is not a heading",
			in:   "Set the value to\nALWAYS or NEVER.",
			want: "Set the value to\nALWAYS or NEVER.",
		},
		{
			name: "indented block becomes a fence and is dedented",
			in:   "Args:\n\n  @file        Read from disk.\n  @-           Read from stdin.",
			want: "Args:\n\n```\n@file        Read from disk.\n@-           Read from stdin.\n```",
		},
		{
			name: "blank lines inside an indented run stay in one fence",
			in:   "  key[]=value\n      Appends a value.\n\n  key[N]=value\n      Assigns at an index.",
			want: "```\nkey[]=value\n    Appends a value.\n\nkey[N]=value\n    Assigns at an index.\n```",
		},
		{
			name: "backslashes survive fencing",
			in:   "  key\\[sub\\]=value    Literal bracket.\n  key[\\\\]=value       Literal backslash.",
			want: "```\nkey\\[sub\\]=value    Literal bracket.\nkey[\\\\]=value       Literal backslash.\n```",
		},
		{
			name: "fence grows past embedded backticks",
			in:   "  use ``` to fence",
			want: "````\nuse ``` to fence\n````",
		},
		{
			name: "trailing whitespace is trimmed",
			in:   "  padded   \n",
			want: "```\npadded\n```",
		},
		{
			name: "crlf input",
			in:   "HEADING\r\n\r\n  block\r\n",
			want: "## Heading\n\n```\nblock\n```",
		},
		{
			name: "prose resumes after an indented block",
			in:   "Before.\n\n  block\n\nAfter.",
			want: "Before.\n\n```\nblock\n```\n\nAfter.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := formatDetail(test.in, 2); got != test.want {
				t.Errorf("formatDetail()\n got: %q\nwant: %q", got, test.want)
			}
		})
	}
}

func TestSentenceCase(t *testing.T) {
	for in, want := range map[string]string{
		"ESCAPING":            "Escaping",
		"REQUEST BODY SYNTAX": "Request body syntax",
		"RAW JSON VALUES":     "Raw JSON values",
		"TOP-LEVEL ARRAY":     "Top-level array",
		"HTTP API":            "HTTP API",
	} {
		if got := sentenceCase(in); got != want {
			t.Errorf("sentenceCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatDetailHeadingLevel(t *testing.T) {
	// md2man renders both "#" and "##" as .SH, so man generation asks for
	// "###" to get a .SS subsection of DESCRIPTION.
	if got, want := formatDetail("ESCAPING\n\nBody.", 3), "### Escaping\n\nBody."; got != want {
		t.Errorf("formatDetail(level 3) = %q, want %q", got, want)
	}
}
