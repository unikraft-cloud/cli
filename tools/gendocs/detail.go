// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "strings"

// demoteHeadings moves the ATX headings in text down until the shallowest one
// is at minLevel. Headings which are already deep enough stay where they are.
func demoteHeadings(text string, minLevel int) string {
	shallowest := shallowestHeading(text)
	if shallowest == 0 || shallowest >= minLevel {
		return text
	}

	delta := minLevel - shallowest
	return mapProseLines(text, func(line string) string {
		return shiftHeading(line, delta)
	})
}

// shallowestHeading returns the level of the shallowest ATX heading in text,
// or zero if it has none.
func shallowestHeading(text string) int {
	shallowest := 0
	mapProseLines(text, func(line string) string {
		if level := headingLevel(line); level > 0 && (shallowest == 0 || level < shallowest) {
			shallowest = level
		}
		return line
	})
	return shallowest
}

// shiftHeading moves one ATX heading down by delta levels, keeping the indent
// and the text after the hashes.
func shiftHeading(line string, delta int) string {
	level := headingLevel(line)
	if level == 0 {
		return line
	}

	indent := headingIndent(line)
	return line[:indent] + strings.Repeat("#", min(level+delta, 6)) + line[indent+level:]
}

// headingLevel returns the level of an ATX heading, or zero if line is not
// one. Four spaces of indent make a code block, not a heading.
func headingLevel(line string) int {
	indent := headingIndent(line)
	if indent > 3 {
		return 0
	}

	rest := line[indent:]
	level := 0
	for level < len(rest) && rest[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0
	}

	// The hashes end the line, or a space separates them from the text.
	if level == len(rest) {
		return level
	}
	switch rest[level] {
	case ' ', '\t', '\r':
		return level
	}
	return 0
}

// headingIndent returns the width of the leading spaces on line.
func headingIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}
