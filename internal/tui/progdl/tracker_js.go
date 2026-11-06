// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package progdl

import (
	"context"
	"io"
)

// TrackImageProgress runs fn. The JavaScript build does not show a progress
// bar.
func TrackImageProgress(ctx context.Context, out io.Writer, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
