// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Args maps gjson paths inside a tool call's arguments to the values they
// must hold: a string, bool or number compared as text, a []string compared
// as a set, Keys compared as a set of resource names, a *regexp.Regexp
// matched against the raw JSON, or Present and Absent.
type Args map[string]any

// Keys are the resource names a key or keys argument must name, each given
// by the model as the bare name or as metro/name.
type Keys []string

type presence bool

const (
	Present presence = true
	Absent  presence = false
)

var pathEscaper = strings.NewReplacer(".", `\.`, "*", `\*`, "?", `\?`, "|", `\|`, "#", `\#`, "@", `\@`)

// Path joins keys into a gjson path, escaping the keys so that a dotted field
// path such as resources.memory stays one key.
func Path(parts ...string) string {
	escaped := make([]string, len(parts))
	for i, part := range parts {
		escaped[i] = pathEscaper.Replace(part)
	}
	return strings.Join(escaped, ".")
}

// Tools gives the names of the called tools in order.
func (r *Result) Tools() []string {
	names := make([]string, 0, len(r.Steps))
	for _, step := range r.Steps {
		names = append(names, step.Tool)
	}
	return names
}

// First gives the first tool call, and fails the test when there is none.
func (r *Result) First(t testing.TB) Step {
	t.Helper()
	require.NotEmpty(t, r.Steps, "no tool was called; the reply was: %s", r.Text)
	return r.Steps[0]
}

// Last gives the last tool call, which is the call an executor stopped the
// run at, and fails the test when there is none.
func (r *Result) Last(t testing.TB) Step {
	t.Helper()
	require.NotEmpty(t, r.Steps, "no tool was called; the reply was: %s", r.Text)
	return r.Steps[len(r.Steps)-1]
}

// Call gives the first call of the tool, and fails the test when there is
// none.
func (r *Result) Call(t testing.TB, tool string) Step {
	t.Helper()
	if calls := r.Calls(tool); len(calls) > 0 {
		return calls[0]
	}
	require.Failf(t, "tool not called", "%s was not called; calls: %v; the reply was: %s", tool, r.Tools(), r.Text)
	return Step{}
}

// Calls gives every call of the tool, in order.
func (r *Result) Calls(tool string) []Step {
	var calls []Step
	for _, step := range r.Steps {
		if step.Tool == tool {
			calls = append(calls, step)
		}
	}
	return calls
}

// Arg looks up a gjson path in the call's arguments.
func (s Step) Arg(path string) gjson.Result {
	return gjson.GetBytes(s.Args, path)
}

// Arguments decodes the call's arguments, for plain assertions such as
// assert.Subset on exact values; Expect matches by pattern and set.
func (s Step) Arguments() map[string]any {
	var arguments map[string]any
	_ = json.Unmarshal(s.Args, &arguments)
	return arguments
}

// Expect checks the call's arguments against want and reports whether they
// all match.
func (s Step) Expect(t testing.TB, want Args) bool {
	t.Helper()
	ok := true
	for _, path := range slices.Sorted(maps.Keys(want)) {
		ok = s.expect(t, path, want[path]) && ok
	}
	return ok
}

func (s Step) expect(t testing.TB, path string, want any) bool {
	t.Helper()
	got := s.Arg(path)
	label := fmt.Sprintf("%s argument %s in %s", s.Tool, path, s.Args)
	switch want := want.(type) {
	case presence:
		if want {
			return assert.True(t, got.Exists(), "%s is missing", label)
		}
		return assert.False(t, got.Exists(), "%s is present", label)
	case *regexp.Regexp:
		return assert.True(t, got.Exists(), "%s is missing", label) &&
			assert.Regexp(t, want, got.Raw, label)
	case []string:
		return assert.True(t, got.Exists(), "%s is missing", label) &&
			assert.ElementsMatch(t, want, items(got), label)
	case Keys:
		if !assert.True(t, got.Exists(), "%s is missing", label) {
			return false
		}
		names := items(got)
		for i, name := range names {
			if _, after, ok := strings.Cut(name, "/"); ok {
				names[i] = after
			}
		}
		return assert.ElementsMatch(t, []string(want), names, label)
	default:
		return assert.True(t, got.Exists(), "%s is missing", label) &&
			assert.Equal(t, fmt.Sprint(want), got.String(), label)
	}
}

func items(got gjson.Result) []string {
	out := make([]string, 0, len(got.Array()))
	for _, item := range got.Array() {
		out = append(out, item.String())
	}
	return out
}
