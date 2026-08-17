// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package interrupt

import (
	"context"
	"sync"
)

// Handler lets callers swap out what a SIGINT cancels, so a long-lived root
// context isn't killed by an interrupt meant for one foreground operation.
type Handler struct {
	mu      sync.Mutex
	current func()
}

func New(rootCancel func()) *Handler {
	return &Handler{current: rootCancel}
}

func (h *Handler) Fire() {
	h.mu.Lock()
	f := h.current
	h.mu.Unlock()
	if f != nil {
		f()
	}
}

func (h *Handler) Set(f func()) (restore func()) {
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

func WithHandler(ctx context.Context, h *Handler) context.Context {
	return context.WithValue(ctx, ctxKey{}, h)
}

func FromContext(ctx context.Context) *Handler {
	h, _ := ctx.Value(ctxKey{}).(*Handler)
	return h
}
