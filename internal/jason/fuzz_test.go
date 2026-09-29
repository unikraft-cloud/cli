// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package jason

import (
	"encoding/json"
	"testing"
)

func FuzzParseItem(f *testing.F) {
	seeds := []string{
		"name=HTTPie",
		"stars:=54000",
		"apps[]=Terminal",
		"[0][type]=platform",
		`{"key":"val"}`,
		`[1,2,3]`,
		"foo[baz][quux]=value",
		"array[]:=1",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = ParseItem(input)
	})
}

func FuzzBuildNestedJSON(f *testing.F) {
	seeds := []string{
		"name=HTTPie\nstars:=54000\napps[]=Terminal",
		"name=HTTPie CLI stars:=54000",
		"[0][type]=platform\n[0][name]=terminal\n[1][type]=platform\n[1][name]=desktop",
		"search[type]=platforms\nsearch[platforms][]=Terminal\nsearch[platforms][1]=Desktop",
		"array[]:=1\narray[1]:=2\narray[2]:=3",
		"{" + `"key":"value"}`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		data, err := buildNestedJSON(input)
		if err != nil {
			return
		}

		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatalf("buildNestedJSON produced invalid JSON: %v\ninput: %q\noutput: %s", err, input, data)
		}
	})
}
