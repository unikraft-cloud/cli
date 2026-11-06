// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package rollout

import (
	"context"
	"time"
)

// Recreate stops the full old set before the new set starts, so the two sets
// never run at the same time.
type Recreate struct {
	// HealthyAfter is how long the new set must run before the old set goes.
	HealthyAfter time.Duration
}

// Apply creates the new set, stops the old set, starts the new set and
// deletes the old set when the new one runs.
func (r Recreate) Apply(ctx context.Context, old Set, create CreateFunc) (bool, error) {
	return apply(ctx, old, create, old.Len(), true, r.HealthyAfter)
}
