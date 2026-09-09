// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"regexp"
	"strings"
)

// headingPattern matches an ALL-CAPS section heading, e.g. "REQUEST BODY
// SYNTAX" or "TOP-LEVEL ARRAY SYNTAX".
var headingPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:[ -][A-Z0-9]+)*$`)

// acronyms keep their capitalisation when an ALL-CAPS heading is lowered to
// sentence case.
var acronyms = map[string]bool{
	"API":   true,
	"CLI":   true,
	"HTTP":  true,
	"HTTPS": true,
	"ID":    true,
	"JSON":  true,
	"OCI":   true,
	"TLS":   true,
	"TUI":   true,
	"URL":   true,
	"UUID":  true,
	"YAML":  true,
}

// formatDetail turns a long command description into Markdown, promoting its
// section headings to the given heading level.
//
// Long descriptions are written for the terminal, in the style of a man page:
// ALL-CAPS section headings and preformatted blocks indented by two or more
// spaces. A Markdown renderer reflows all of that as prose, which collapses
// the aligned columns onto one line and swallows the backslashes that document
// escaping. Headings become sections and indented runs become fenced code
// blocks, so both the generated MDX and the man pages keep the layout the text
// was written with.
//
// The heading level matters for the man pages: md2man renders both "#" and
// "##" as .SH, which would make each section a sibling of DESCRIPTION rather
// than a part of it, so man generation asks for "###" and gets .SS instead.
func formatDetail(text string, level int) string {
	heading := strings.Repeat("#", level) + " "
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	var out []string
	appendBlank := func() {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}

	for i := 0; i < len(lines); {
		line := lines[i]
		switch {
		case strings.TrimSpace(line) == "":
			appendBlank()
			i++

		case isIndented(line):
			block, next := indentedBlock(lines, i)
			appendBlank()
			out = append(out, fence(block)...)
			out = append(out, "")
			i = next

		case isHeading(lines, i):
			appendBlank()
			out = append(out, heading+sentenceCase(strings.TrimSpace(line)))
			out = append(out, "")
			i++

		default:
			out = append(out, line)
			i++
		}
	}

	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// isIndented reports whether line begins a preformatted block, i.e. it carries
// at least two leading spaces.
func isIndented(line string) bool {
	return strings.HasPrefix(line, "  ")
}

// isHeading reports whether the line at index i is a section heading: an
// ALL-CAPS line standing alone in its own paragraph.
func isHeading(lines []string, i int) bool {
	if !headingPattern.MatchString(strings.TrimRight(lines[i], " ")) {
		return false
	}
	if i > 0 && strings.TrimSpace(lines[i-1]) != "" {
		return false
	}
	return i == len(lines)-1 || strings.TrimSpace(lines[i+1]) == ""
}

// indentedBlock collects the run of indented lines starting at index i,
// including any blank lines it straddles, and returns it dedented along with
// the index of the first line after the run.
func indentedBlock(lines []string, i int) ([]string, int) {
	end := i
	for j := i; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "" {
			continue
		}
		if !isIndented(lines[j]) {
			break
		}
		end = j
	}

	block := lines[i : end+1]
	indent := -1
	for _, line := range block {
		if strings.TrimSpace(line) == "" {
			continue
		}
		width := len(line) - len(strings.TrimLeft(line, " "))
		if indent == -1 || width < indent {
			indent = width
		}
	}

	dedented := make([]string, 0, len(block))
	for _, line := range block {
		if len(line) < indent {
			dedented = append(dedented, "")
			continue
		}
		dedented = append(dedented, strings.TrimRight(line[indent:], " "))
	}
	return dedented, end + 1
}

// fence wraps block in a code fence long enough to survive any backticks the
// block itself contains.
func fence(block []string) []string {
	longest := 0
	for _, line := range block {
		run := 0
		for _, r := range line {
			if r == '`' {
				run++
				longest = max(longest, run)
			} else {
				run = 0
			}
		}
	}

	delim := strings.Repeat("`", max(3, longest+1))
	fenced := make([]string, 0, len(block)+2)
	fenced = append(fenced, delim)
	fenced = append(fenced, block...)
	return append(fenced, delim)
}

// sentenceCase lowers an ALL-CAPS heading to sentence case, leaving acronyms
// alone: "NESTED JSON SYNTAX" becomes "Nested JSON syntax".
func sentenceCase(heading string) string {
	words := strings.Split(heading, " ")
	for i, word := range words {
		parts := strings.Split(word, "-")
		for j, part := range parts {
			if !acronyms[part] {
				parts[j] = strings.ToLower(part)
			}
		}
		words[i] = strings.Join(parts, "-")
	}

	out := strings.Join(words, " ")
	return strings.ToUpper(out[:1]) + out[1:]
}
