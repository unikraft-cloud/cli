// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package jason

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxArrayIndex = 10_000

// Item is one parsed nested-item entry.
type Item struct {
	// Path holds the bracketed segments.
	Path []string
	// Value holds the right-hand side.
	Value string
	// Raw reports whether the value came from ':=', or is a root JSON
	// literal, and should be decoded as JSON.
	Raw bool
}

// buildNestedJSON converts nested-item input into JSON bytes.
func buildNestedJSON(input string) ([]byte, error) {
	items, err := parseItems(input)
	if err != nil {
		return nil, err
	}
	root, err := buildTree(items)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return data, nil
}

// parseItems splits and parses whitespace-separated nested-item input.
func parseItems(input string) ([]Item, error) {
	items, err := splitItems(input)
	if err != nil {
		return nil, err
	}
	parsed := make([]Item, 0, len(items))
	for _, item := range items {
		p, err := ParseItem(item)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, p)
	}
	return parsed, nil
}

// buildTree assigns every item into an intermediate tree, merging root-level
// JSON literals when needed. Literal ('=') values are left as [literal].
func buildTree(parsed []Item) (any, error) {
	hasArrayRoot := slices.ContainsFunc(parsed, func(p Item) bool {
		return len(p.Path) > 0 && p.Path[0] == ""
	})

	var root any
	if hasArrayRoot {
		root = make([]any, 0)
	} else {
		root = make(map[string]any)
	}

	for _, p := range parsed {
		var val any
		if p.Raw {
			if err := json.Unmarshal([]byte(p.Value), &val); err != nil {
				return nil, fmt.Errorf("invalid raw JSON value %q: %w", p.Value, err)
			}
		} else {
			val = literal(p.Value)
		}
		path := p.Path
		if len(path) > 0 && path[0] == "" {
			path = path[1:]
		}
		// Empty path means a JSON literal (object or array) that should
		// be merged at the root.
		if len(path) == 0 {
			if m, ok := val.(map[string]any); ok {
				if hasArrayRoot {
					return nil, fmt.Errorf("cannot merge a JSON object literal into an array-rooted body: %q", p.Value)
				}
				rootMap, ok := root.(map[string]any)
				if !ok {
					root = make(map[string]any)
					rootMap = root.(map[string]any)
				}
				maps.Copy(rootMap, m)
			} else {
				root = val
			}
			continue
		}
		var err error
		root, err = assignAtPath(root, path, val)
		if err != nil {
			return nil, err
		}
	}

	return root, nil
}

// scanSegment reads one path segment from the start of s.
//
// Parsing stops at '[', ']', '=', or ':='. Backslash escapes the next
// character so delimiters can appear inside keys. A backslash before a digit
// is preserved so callers can distinguish escaped digit keys from array
// indexes.
func scanSegment(s string) (string, int) {
	var buf strings.Builder
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == '\\' && i+1 < len(s) {
			next := s[i+1]
			if next >= '0' && next <= '9' {
				buf.WriteByte('\\')
			}
			buf.WriteByte(next)
			i += 2
			continue
		}
		if ch == '[' || ch == ']' || ch == '=' || (ch == ':' && i+1 < len(s) && s[i+1] == '=') {
			break
		}
		buf.WriteByte(ch)
		i++
	}
	return buf.String(), i
}

// splitItems breaks a whitespace-separated input string into individual items.
//
// It preserves spaces inside values until the next valid item boundary.
func splitItems(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	var items []string
	for len(s) > 0 {
		if item, rest, ok := splitItemPrefix(s); ok {
			items = append(items, item)
			s = rest
			continue
		}
		if _, err := ParseItem(s); err != nil {
			return nil, err
		}
		return append(items, s), nil
	}

	return items, nil
}

// splitItemPrefix returns the leading item in s when the remainder starts a
// new item after whitespace.
func splitItemPrefix(s string) (item string, rest string, ok bool) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			candidate := s[:i]
			if candidate != "" {
				if p, err := ParseItem(candidate); err == nil && isCompleteCandidate(p) {
					next := trimLeftIndex(s, i)
					if next >= len(s) || startsItem(s[next:]) {
						return candidate, s[next:], true
					}
				}
			}
			for i < len(s) {
				r, size = utf8.DecodeRuneInString(s[i:])
				if !unicode.IsSpace(r) {
					break
				}
				i += size
			}
			continue
		}
		i += size
	}
	return "", "", false
}

// isCompleteCandidate reports whether p is safe to treat as a finished item
// boundary while scanning whitespace-separated input.
//
// Literal ('=') values are arbitrary text, so any prefix is a plausible
// boundary. Raw (':=') values must decode as JSON, so a truncated fragment
// such as `{"a":` must never be accepted as a boundary merely because some
// later token happens to look like the start of a new item — otherwise a
// value like `data:={"a": 1 , "b": 2}` can be split apart at the interior
// space before the comma.
func isCompleteCandidate(p Item) bool {
	if !p.Raw {
		return true
	}
	return json.Valid([]byte(p.Value))
}

// startsItem reports whether s begins with a valid nested-item input.
func startsItem(s string) bool {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	if s == "" {
		return false
	}
	if _, _, ok := splitJSONLiteralItem(s); ok {
		return true
	}
	if i := strings.IndexFunc(s, unicode.IsSpace); i >= 0 {
		s = s[:i]
	}
	_, err := ParseItem(s)
	return err == nil
}

// trimLeftIndex skips leading whitespace and returns the first non-space byte
// index in s starting at start.
func trimLeftIndex(s string, start int) int {
	for start < len(s) {
		r, size := utf8.DecodeRuneInString(s[start:])
		if !unicode.IsSpace(r) {
			break
		}
		start += size
	}
	return start
}

// splitJSONLiteralItem returns the longest valid JSON literal prefix in s.
func splitJSONLiteralItem(s string) (item string, rest string, ok bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return "", "", false
	}
	offset := int(dec.InputOffset())
	if offset < len(s) {
		r, _ := utf8.DecodeRuneInString(s[offset:])
		if !unicode.IsSpace(r) {
			return "", "", false
		}
	}
	return s[:offset], s[offset:], true
}

// isJSONLiteral reports whether trimmed is a valid JSON object or array.
func isJSONLiteral(trimmed string) bool {
	return len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') && json.Valid([]byte(trimmed))
}

// parsePathSegments parses the bracketed path prefix from one item.
func parsePathSegments(item string) ([]string, int, error) {
	var segments []string
	pos := 0

	seg, n := scanSegment(item)
	pos += n
	if seg != "" {
		segments = append(segments, seg)
	} else if pos < len(item) && (item[pos] == '[' || item[pos] == '=' || (item[pos] == ':' && pos+1 < len(item) && item[pos+1] == '=')) {
		segments = append(segments, "")
	}

	for pos < len(item) && item[pos] == '[' {
		pos++
		seg, n = scanSegment(item[pos:])
		pos += n
		if pos >= len(item) || item[pos] != ']' {
			return nil, 0, fmt.Errorf("invalid path in %q: unclosed bracket", item)
		}
		pos++
		segments = append(segments, seg)
	}

	return segments, pos, nil
}

// parseAssignmentOp parses the assignment operator after the path.
func parseAssignmentOp(item string, pos int) (isRaw bool, valuePos int, err error) {
	switch {
	case pos+1 < len(item) && item[pos:pos+2] == ":=":
		return true, pos + 2, nil
	case pos < len(item) && item[pos] == '=':
		return false, pos + 1, nil
	default:
		return false, 0, fmt.Errorf("invalid item: %s", item)
	}
}

// ParseItem splits a single item string into path segments and value.
// Input forms:
//
//	foo=bar           → Path=["foo"],          Value="bar",   Raw=false
//	foo:=42           → Path=["foo"],          Value="42",    Raw=true
//	foo[bar][]=x      → Path=["foo","bar",""], Value="x",     Raw=false
//	[0][key]=val      → Path=["0","key"],       Value="val",  Raw=false
//	{"key":"val"}     → Path=[],               Value=JSON,    Raw=true
//	[1,2,3]           → Path=[],               Value=JSON,    Raw=true
//
// Supported forms include:
//   - key=value for literal strings
//   - key:=value for raw JSON values
//   - key[sub]=value for nested objects and arrays
//   - standalone JSON objects or arrays at the root
func ParseItem(item string) (Item, error) {
	trimmed := strings.TrimSpace(item)
	if isJSONLiteral(trimmed) {
		return Item{
			Path:  nil,
			Value: trimmed,
			Raw:   true,
		}, nil
	}

	segments, pos, err := parsePathSegments(item)
	if err != nil {
		return Item{}, err
	}

	isRaw, valuePos, err := parseAssignmentOp(item, pos)
	if err != nil {
		return Item{}, err
	}

	return Item{Path: segments, Value: item[valuePos:], Raw: isRaw}, nil
}

// assignAtPath walks path and assigns val at the leaf container.
func assignAtPath(node any, path []string, val any) (any, error) {
	if len(path) == 0 {
		return val, nil
	}
	return assignAt(node, path[0], path[1:], val)
}

// assignAt routes an assignment to the concrete container type.
func assignAt(node any, seg string, rest []string, val any) (any, error) {
	switch n := node.(type) {
	case map[string]any:
		return assignAtMap(n, seg, rest, val)
	case []any:
		return assignAtSlice(n, seg, rest, val)
	}
	return nil, fmt.Errorf("unexpected container type %T at segment %q", node, seg)
}

// assignAtMap assigns val into the map entry selected by seg.
func assignAtMap(m map[string]any, seg string, rest []string, val any) (any, error) {
	key := cleanKey(seg)
	if len(rest) == 0 {
		m[key] = val
		return m, nil
	}
	child, err := childOrNew(m[key], rest[0])
	if err != nil {
		return nil, err
	}
	result, err := assignAt(child, rest[0], rest[1:], val)
	if err != nil {
		return nil, err
	}
	m[key] = result
	return m, nil
}

// assignAtSlice assigns val into the slice entry selected by seg.
//
// An empty segment appends, a numeric segment writes by index, and any
// remaining path is assigned recursively under that child.
func assignAtSlice(s []any, seg string, rest []string, val any) (any, error) {
	if seg == "" {
		return assignAtSliceAppend(s, rest, val)
	}
	idx64, err := strconv.ParseInt(seg, 10, 64)
	if err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
			return nil, fmt.Errorf("array index %q exceeds maximum allowed index %d", seg, maxArrayIndex)
		}
		return nil, fmt.Errorf("type error: cannot perform key-based access on array at segment %q", seg)
	}
	idx := int(idx64)
	if idx < 0 || idx > maxArrayIndex {
		return nil, fmt.Errorf("array index %d exceeds maximum allowed index %d", idx, maxArrayIndex)
	}
	for len(s) <= idx {
		s = append(s, nil)
	}
	if len(rest) == 0 {
		s[idx] = val
		return s, nil
	}
	child, err := childOrNew(s[idx], rest[0])
	if err != nil {
		return nil, err
	}
	result, err := assignAt(child, rest[0], rest[1:], val)
	if err != nil {
		return nil, err
	}
	s[idx] = result
	return s, nil
}

// assignAtSliceAppend appends val to s.
//
// When more path segments remain, it creates the next container, fills it,
// and appends the resulting value.
func assignAtSliceAppend(s []any, rest []string, val any) (any, error) {
	if len(rest) == 0 {
		return append(s, val), nil
	}
	child := newContainer(rest[0])
	result, err := assignAt(child, rest[0], rest[1:], val)
	if err != nil {
		return nil, err
	}
	return append(s, result), nil
}

// childOrNew returns an existing child container or creates one for nextSeg.
func childOrNew(child any, nextSeg string) (any, error) {
	isNextArray := nextSeg == "" || isNumericKey(nextSeg)
	switch c := child.(type) {
	case []any:
		if !isNextArray {
			return nil, fmt.Errorf("type error: cannot perform key-based access on array")
		}
		return c, nil
	case map[string]any:
		if isNextArray {
			return nil, fmt.Errorf("type error: cannot perform index-based access on object")
		}
		return c, nil
	default:
		return newContainer(nextSeg), nil
	}
}

// newContainer creates a container for the next path segment.
//
// Empty and numeric segments create slices. All other segments create maps,
// including escaped digit keys such as "\1".
func newContainer(nextSeg string) any {
	if nextSeg == "" || isNumericKey(nextSeg) {
		return make([]any, 0)
	}
	return make(map[string]any)
}

// isNumericKey reports whether s is an unescaped decimal index.
func isNumericKey(s string) bool {
	if s == "" || s[0] == '\\' {
		return false
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

// cleanKey removes the escape marker from digit keys like "\1".
func cleanKey(seg string) string {
	if len(seg) >= 2 && seg[0] == '\\' {
		if _, err := strconv.Atoi(seg[1:]); err == nil {
			return seg[1:]
		}
	}
	return seg
}
