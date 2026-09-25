// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import "fmt"

// notAvailable reports that a command does not work on Windows yet.
func notAvailable(what string) error {
	return fmt.Errorf("%s is not available on Windows yet; use the unikraft CLI in WSL", what)
}
