// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

type TUICmd struct {
	Resource string `arg:"" optional:"" help:"Resource type to browse."`
	Name     string `arg:"" optional:"" help:"Resource key to open."`
}
