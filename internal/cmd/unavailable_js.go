// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import "fmt"

// notAvailable reports that a command needs a local environment which the
// browser build does not provide.  It names the native CLI as the alternative.
func notAvailable(what string) error {
	return fmt.Errorf(
		"%s is not available in the web console; use the native unikraft CLI: https://unikraft.com/docs/cli",
		what,
	)
}
