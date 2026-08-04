// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package interrupt

import (
	"context"
	"sync"
)

// InterruptHandler lets callers swap out what a SIGINT actually cancels, so that a
// long-lived root context isn't permanently killed by an interrupt meant for
// a single foreground operation (e.g. a command running inside the shell).
type InterruptHandler struct {
	mu      sync.Mutex
	current func()
}

func New(rootCancel func()) *InterruptHandler {
	return &InterruptHandler{current: rootCancel}
}

func (h *InterruptHandler) Fire() {
	h.mu.Lock()
	f := h.current
	h.mu.Unlock()
	if f != nil {
		f()
	}
}

func (h *InterruptHandler) Set(f func()) (restore func()) {
	h.mu.Lock()
	prev := h.current
	h.current = f
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		h.current = prev
		h.mu.Unlock()
	}
}

type ctxKey struct{}

func WithHandler(ctx context.Context, h *InterruptHandler) context.Context {
	return context.WithValue(ctx, ctxKey{}, h)
}

func FromContext(ctx context.Context) *InterruptHandler {
	h, _ := ctx.Value(ctxKey{}).(*InterruptHandler)
	return h
}
