// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "strings"

// escapeMdx escapes '<' and '{'/'}' in prose so MDX doesn't parse them as
// JSX, leaving fenced code blocks and inline code spans untouched.
func escapeMdx(text string) string {
	return mapProseLines(text, escapeMdxLine)
}

// mapProseLines applies transform to each line of text which is outside a
// fenced code block.
func mapProseLines(text string, transform func(string) string) string {
	lines := strings.Split(text, "\n")
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if closesFence(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if f := opensFence(trimmed); f != "" {
			fence = f
			continue
		}
		lines[i] = transform(line)
	}
	return strings.Join(lines, "\n")
}

// opensFence returns the fence delimiter if trimmed opens a code block.
func opensFence(trimmed string) string {
	for _, ch := range []byte{'`', '~'} {
		n := 0
		for n < len(trimmed) && trimmed[n] == ch {
			n++
		}
		if n >= 3 {
			return trimmed[:n]
		}
	}
	return ""
}

// closesFence reports whether trimmed closes the given fence.
func closesFence(trimmed, fence string) bool {
	ch := fence[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	return n == len(trimmed) && n >= len(fence)
}

// escapeMdxLine escapes a single line, skipping over inline code spans.
func escapeMdxLine(line string) string {
	var out strings.Builder
	for i := 0; i < len(line); {
		c := line[i]
		if c == '`' {
			j := i
			for j < len(line) && line[j] == '`' {
				j++
			}
			delim := line[i:j]
			if end := strings.Index(line[j:], delim); end != -1 {
				out.WriteString(line[i : j+end+len(delim)])
				i = j + end + len(delim)
				continue
			}
		}
		if c == '<' || c == '{' || c == '}' {
			out.WriteByte('\\')
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}
