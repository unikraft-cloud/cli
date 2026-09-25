// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import kongcompletion "github.com/jotaen/kong-completion"

// CompletionCmd prints the shell code that activates tab completion.  It adds
// PowerShell to the shells that kong-completion knows.
type CompletionCmd struct {
	Shell string `arg:"" help:"The name of the shell you are using" enum:"bash,zsh,fish,powershell," default:""`
	Code  bool   `short:"c" help:"Generate the initialization code"`
}

func (c *CompletionCmd) Help() string {
	return (&kongcompletion.Completion{}).Help()
}
