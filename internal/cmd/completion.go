// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import kongcompletion "github.com/jotaen/kong-completion"

// CompletionCmd wraps the kong-completion command.
type CompletionCmd struct {
	kongcompletion.Completion `embed:""`
}
