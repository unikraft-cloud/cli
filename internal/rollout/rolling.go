// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package rollout

import (
	"context"
)

// Rolling replaces the old set one batch at a time, and starts each new batch
// while the old instances still run.
type Rolling struct {
	// Batch is how many instances each step replaces. It is at least 1.
	Batch int
}

// Apply stops each old batch when its new batch runs, and deletes the old set
// at the end.
func (r Rolling) Apply(ctx context.Context, old Set, create CreateFunc) (bool, error) {
	return apply(ctx, old, create, max(r.Batch, 1))
}
