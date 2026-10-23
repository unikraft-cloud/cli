// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

// Subpanels allows a resource to provide detail subpanels.
type Subpanels interface {
	Subpanels(ctx context.Context, key string) []tea.Model
}
