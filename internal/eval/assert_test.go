// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval_test

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"unikraft.com/cli/internal/eval"
)

// quiet swallows assertion failures so that the tests can check Expect's
// verdict on arguments that must not match.
type quiet struct {
	testing.TB
}

func (quiet) Errorf(string, ...any) {}

func (quiet) Helper() {}

func TestPath(t *testing.T) {
	assert.Equal(t, "keys", eval.Path("keys"))
	assert.Equal(t, `set.resources\.memory`, eval.Path("set", "resources.memory"))
	assert.Equal(t, `set.a\*b\?c`, eval.Path("set", "a*b?c"))
}

func TestExpect(t *testing.T) {
	step := eval.Step{
		Tool: "instance_create",
		Args: json.RawMessage(`{
			"set": {
				"name": "edge",
				"autostart": false,
				"resources.memory": 1024,
				"plugins": ["name=sandbox,image=plugins/sandbox:latest"]
			},
			"dry_run": "true",
			"keys": ["a", "b"]
		}`),
	}

	assert.True(t, step.Expect(t, eval.Args{
		"set.name":                           "edge",
		"set.autostart":                      false,
		eval.Path("set", "resources.memory"): 1024,
		"set.plugins":                        regexp.MustCompile(`name=sandbox`),
		"dry_run":                            true,
		"keys":                               []string{"b", "a"},
		"set.image":                          eval.Absent,
		"set":                                eval.Present,
	}))

	qualified := eval.Step{Tool: "instance_delete", Args: json.RawMessage(`{"keys": ["fra/web-1", "web-2"], "key": "fra/web-3"}`)}
	assert.True(t, qualified.Expect(t, eval.Args{"keys": eval.Keys{"web-2", "web-1"}, "key": eval.Keys{"web-3"}}))
	assert.False(t, qualified.Expect(quiet{t}, eval.Args{"keys": eval.Keys{"web-1"}}))
	assert.False(t, qualified.Expect(quiet{t}, eval.Args{"keys": []string{"web-1", "web-2"}}))

	for name, want := range map[string]eval.Args{
		"value":     {"set.name": "other"},
		"bool":      {"set.autostart": true},
		"number":    {eval.Path("set", "resources.memory"): 512},
		"regexp":    {"set.plugins": regexp.MustCompile(`name=other`)},
		"set":       {"keys": []string{"a"}},
		"present":   {"set.image": eval.Present},
		"absent":    {"set.name": eval.Absent},
		"missing":   {"set.image": "nginx"},
		"unescaped": {"set.resources.memory": 1024},
	} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, step.Expect(quiet{t}, want))
		})
	}
}

func TestResultCalls(t *testing.T) {
	res := &eval.Result{Steps: []eval.Step{
		{Tool: "instance_list", Args: json.RawMessage(`{"keys":["web-1"]}`)},
		{Tool: "instance_control", Args: json.RawMessage(`{"action":"stop","keys":["web-1"]}`)},
	}}

	assert.Equal(t, []string{"instance_list", "instance_control"}, res.Tools())
	assert.Equal(t, "instance_list", res.First(t).Tool)
	assert.Equal(t, "instance_control", res.Last(t).Tool)
	assert.Equal(t, "stop", res.Call(t, "instance_control").Arg("action").String())
	assert.Len(t, res.Calls("instance_list"), 1)
	assert.Empty(t, res.Calls("volume_create"))
	assert.Equal(t, map[string]any{"action": "stop", "keys": []any{"web-1"}}, res.Call(t, "instance_control").Arguments())
	assert.Subset(t, res.Call(t, "instance_control").Arguments(), map[string]any{"action": "stop"})
	assert.Empty(t, new(eval.Result).Tools())
}
