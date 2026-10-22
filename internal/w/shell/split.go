// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Split splits s into fields like [mvdan.cc/sh/v3/shell.Fields], applying
// quote removal but passing all expansions (parameters, substitutions, braces,
// globs and tildes) through literally.
func Split(s string) ([]string, error) {
	literal := func(part syntax.WordPart) *syntax.SglQuoted {
		return &syntax.SglQuoted{Value: s[part.Pos().Offset():part.End().Offset()]}
	}

	var fields []string
	for word, err := range syntax.NewParser().WordsSeq(strings.NewReader(s)) {
		if err != nil {
			return nil, err
		}
		for i, part := range word.Parts {
			switch part := part.(type) {
			case *syntax.SglQuoted:
			case *syntax.Lit:
				word.Parts[i] = &syntax.SglQuoted{Value: unescapeUnquoted(part.Value)}
			case *syntax.DblQuoted:
				for j, inner := range part.Parts {
					if _, ok := inner.(*syntax.Lit); !ok {
						part.Parts[j] = literal(inner)
					}
				}
			default:
				word.Parts[i] = literal(part)
			}
		}
		field, err := expand.Literal(nil, word)
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func unescapeUnquoted(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			if s[i] == '\n' {
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
