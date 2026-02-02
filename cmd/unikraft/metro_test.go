// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "testing"

func metroTests(t *testing.T, r *testRunner) {
	t.Run("help", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "metro", "--help"}},
			{args: []string{unikraftCmd, "metro", "get", "--help"}},
			{args: []string{unikraftCmd, "metro", "list", "--help"}},
			{args: []string{unikraftCmd, "metro", "create", "--help"}},
			{args: []string{unikraftCmd, "metro", "edit", "--help"}},
			{args: []string{unikraftCmd, "metro", "delete", "--help"}},
		})
	})

	t.Run("create", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "metro", "list"}},
				// The create command resolves quotas/status against the
				// endpoint. For a fake metro this fails, but the metro is
				// still persisted.
				{args: []string{
					unikraftCmd, "metro", "create",
					"--set", "name=example",
					"--set", "endpoint=https://api.example.unikraft.cloud",
					"--set", "country=se",
				}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "list"}},
				{args: []string{unikraftCmd, "metro", "get", "example", "-f", "name,country,endpoint"}},
			})
	})
	t.Run("create-shortcut", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "metro", "list"}},
				{args: []string{
					unikraftCmd, "metro", "create",
					"--name", "example",
					"--endpoint", "https://api.example.unikraft.cloud",
					"--country", "se",
				}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "list"}},
				{args: []string{unikraftCmd, "metro", "get", "example", "-f", "name,country,endpoint"}},
			})
	})
	t.Run("create-duplicate", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "metro", "create",
					"--name", "example",
					"--endpoint", "https://api.example.unikraft.cloud",
					"--country", "se",
				}, allowErr: true},
				{args: []string{
					unikraftCmd, "metro", "create",
					"--name", "example",
					"--endpoint", "https://api.example2.unikraft.cloud",
					"--country", "se",
				}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "list"}},
			})
	})

	t.Run("edit", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "metro", "create",
					"--set", "name=example",
					"--set", "endpoint=https://api.example.unikraft.cloud",
					"--set", "country=se",
				}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "get", "example", "-f", "name,country,endpoint"}},
				{args: []string{
					unikraftCmd, "metro", "edit", "example",
					"--set", "endpoint=https://api.example2.unikraft.cloud",
				}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "get", "example", "-f", "name,country,endpoint"}},
			})
	})
	t.Run("edit-shortcut", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "metro", "create",
					"--name", "example",
					"--endpoint", "https://api.example.unikraft.cloud",
					"--country", "se",
				}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "get", "example", "-f", "name,country,endpoint"}},
				{args: []string{
					unikraftCmd, "metro", "edit", "example",
					"--endpoint", "https://api.example2.unikraft.cloud",
					"--country", "SE",
				}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "get", "example", "-f", "name,country,endpoint"}},
			})
	})

	t.Run("delete", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "metro", "create",
					"--set", "name=example",
					"--set", "endpoint=https://api.example.unikraft.cloud",
					"--set", "country=se",
				}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "list"}},
				{args: []string{unikraftCmd, "metro", "delete", "example"}},
				{args: []string{unikraftCmd, "metro", "list"}},
			})
	})
	t.Run("delete-nonexistent", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "metro", "delete", "nonexistent"}, allowErr: true},
			})
	})
}
