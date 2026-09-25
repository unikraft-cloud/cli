// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package cli is the CLI's own commands as the instance the shell builtins act on, for programs other than the CLI.
package cli

import (
	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/pkg/shellbuiltins"
)

func Instance(key string) shellbuiltins.Instance {
	return cmd.ShellInstance{Key: key}
}
