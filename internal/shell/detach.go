// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import "context"

type detachedKey struct{}

// Detached marks a context whose command nobody waits for: a probe, not a typed command.
func Detached(ctx context.Context) context.Context {
	return context.WithValue(ctx, detachedKey{}, true)
}

func IsDetached(ctx context.Context) bool {
	detached, _ := ctx.Value(detachedKey{}).(bool)
	return detached
}
