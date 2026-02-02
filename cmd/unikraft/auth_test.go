// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "testing"

func authTests(t *testing.T, r *testRunner) {
	t.Run("help", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "login", "--help"}},
			{args: []string{unikraftCmd, "logout", "--help"}},
			{args: []string{unikraftCmd, "profile", "--help"}},
			{args: []string{unikraftCmd, "profile", "get", "--help"}},
			{args: []string{unikraftCmd, "profile", "list", "--help"}},
			{args: []string{unikraftCmd, "profile", "use", "--help"}},
			{args: []string{unikraftCmd, "profile", "create", "--help"}},
			{args: []string{unikraftCmd, "profile", "delete", "--help"}},
		})
	})
	t.Run("flow", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "login", "--check"}},
				{args: []string{unikraftCmd, "profile", "list"}},
				{args: []string{unikraftCmd, "metro", "list"}},
				{args: []string{unikraftCmd, "logout"}},
				{args: []string{unikraftCmd, "profile", "list"}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "list"}, allowErr: true},
			})
	})

	t.Run("profile-create", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "profile", "list"}},
				{args: []string{
					unikraftCmd, "profile", "create",
					"--name", "test-profile",
					"--token", "test-token",
					"--organization", "test-org",
				}},
				{args: []string{unikraftCmd, "profile", "list"}},
				{args: []string{unikraftCmd, "profile", "get", "test-profile"}},
			})
	})
	t.Run("profile-create-duplicate", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "profile", "create",
					"--name", "dup-profile",
					"--token", "test-token",
				}},
				{args: []string{
					unikraftCmd, "profile", "create",
					"--name", "dup-profile",
					"--token", "test-token-2",
				}, allowErr: true},
			})
	})
	t.Run("profile-delete", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "profile", "create",
					"--name", "to-delete",
					"--token", "test-token",
				}},
				{args: []string{unikraftCmd, "profile", "list"}},
				{args: []string{unikraftCmd, "profile", "delete", "to-delete"}},
				{args: []string{unikraftCmd, "profile", "list"}},
			})
	})
	t.Run("profile-delete-active", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				// The default profile from the online config is active;
				// deleting it should fail.
				{args: []string{unikraftCmd, "profile", "list"}},
				{args: []string{unikraftCmd, "profile", "delete", "default"}, allowErr: true},
				{args: []string{unikraftCmd, "profile", "list"}},
			})
	})
}
