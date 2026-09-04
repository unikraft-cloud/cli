// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import "strings"

const shellSpecial = "|&;<>()$"

func highlight(line string, isBuiltin func(string) bool) string {
	var out strings.Builder
	out.Grow(len(line) * 2)

	for i := 0; i < len(line); {
		switch c := line[i]; {
		case c == '\'' || c == '"':
			end := strings.IndexByte(line[i+1:], c)
			if end < 0 {
				end = len(line)
			} else {
				end = i + 1 + end + 1
			}
			out.WriteString(highlightStringStyle.Render(line[i:end]))
			i = end

		case strings.IndexByte(shellSpecial, c) >= 0:
			out.WriteString(highlightSpecialStyle.Render(string(c)))
			i++

		case c == ' ' || c == '\t':
			end := i + 1
			for end < len(line) && (line[end] == ' ' || line[end] == '\t') {
				end++
			}
			out.WriteString(line[i:end])
			i = end

		case c == BuiltinSigil[0]:
			end := i + wordLen(line[i:])
			word := line[i:end]
			if isBuiltin != nil && isBuiltin(strings.TrimPrefix(word, BuiltinSigil)) {
				out.WriteString(highlightBuiltinStyle.Render(word))
			} else {
				out.WriteString(word)
			}
			i = end

		default:
			end := i + wordLen(line[i:])
			out.WriteString(line[i:end])
			i = end
		}
	}
	return out.String()
}

func wordLen(s string) int {
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '\'' || c == '"' || c == ' ' || c == '\t' || strings.IndexByte(shellSpecial, c) >= 0 {
			return i
		}
	}
	return len(s)
}
