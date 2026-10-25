// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import (
	"context"

	"unikraft.com/cli/internal/config"
)

func runGraphLive(context.Context, config.Stdio, *graphSession, int) error {
	return notAvailable("the live chart of instance graph (use --samples or --output)")
}
