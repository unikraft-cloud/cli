// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import "github.com/alecthomas/kong"

func (c *CompletionCmd) Run(ctx *kong.Context) error {
	return notAvailable("completion")
}
