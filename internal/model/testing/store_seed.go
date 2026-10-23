// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package testing

// BaseTestStore returns a fresh store for CLI resource cmd tests.
//
// Note: this is intentionally a function (not a var) so callers get a new map
// and slices each time.
func BaseTestStore() map[string]TestModel {
	return map[string]TestModel{
		"test1": {
			ID:        "id-test1",
			Name:      "test1",
			State:     "pending",
			URL:       "https://example.com",
			Hidden:    "hidden-test1",
			Invisible: "invisible-test1",
			Settings:  TestSettings{Foo: 42, Bar: "hello"},
			Authors:   []TestAuthor{{Name: "Alice", Email: "alice@example.com"}, {Name: "Bob", Email: "bob@example.com"}},
		},
		"test2": {
			ID:        "id-test2",
			Name:      "test2",
			State:     "pending",
			URL:       "https://example.org",
			Hidden:    "hidden-test2",
			Invisible: "invisible-test2",
			Settings:  TestSettings{Foo: 7, Bar: "world"},
			Authors:   []TestAuthor{{Name: "Charlie", Email: "charlie@example.com"}, {Name: "Dana", Email: "dana@example.com"}},
		},
	}
}
