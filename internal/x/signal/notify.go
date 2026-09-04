// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package signal

import (
	"context"
	"os"
	"os/signal"
	"slices"
	"sync"
)

type controllerKey struct{}

// controller is the registration a context was built with, kept reachable so
// that a command needing a signal for itself can take it and give it back.
type controller struct {
	ch chan os.Signal

	mu        sync.Mutex
	suspended []os.Signal
}

func (c *controller) holds(sig os.Signal) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !slices.Contains(c.suspended, sig)
}

func NotifyContext(parent context.Context, sig ...os.Signal) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)

	c := &controller{ch: make(chan os.Signal, len(sig)+1)}
	signal.Notify(c.ch, sig...)

	ctx = context.WithValue(ctx, controllerKey{}, c)
	go func() {
		for {
			select {
			case received := <-c.ch:
				if c.holds(received) {
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return ctx, func() {
		signal.Stop(c.ch)
		cancel()
	}
}

func Suspend(ctx context.Context, sig ...os.Signal) (restore func()) {
	c, ok := ctx.Value(controllerKey{}).(*controller)
	if !ok {
		return func() {}
	}

	c.mu.Lock()
	c.suspended = append(c.suspended, sig...)
	c.mu.Unlock()

	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		for _, s := range sig {
			if i := slices.Index(c.suspended, s); i >= 0 {
				c.suspended = slices.Delete(c.suspended, i, i+1)
			}
		}
	}
}
