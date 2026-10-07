// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sync

import (
	"context"
	"sync"
)

// OnceCtxValues runs f once and caches its results.
// The context from the first call is used for the execution.
func OnceCtxValues[T any](f func(context.Context) (T, error)) func(context.Context) (T, error) {
	var (
		once sync.Once
		v    T
		err  error
	)
	return func(ctx context.Context) (T, error) {
		once.Do(func() {
			v, err = f(ctx)
		})
		return v, err
	}
}
