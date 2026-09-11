// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package signal

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	settle = 200 * time.Millisecond
	arrive = 2 * time.Second
	tick   = 10 * time.Millisecond
)

func raise(t *testing.T, sig syscall.Signal) {
	t.Helper()
	require.NoError(t, syscall.Kill(syscall.Getpid(), sig))
}

func TestNotifyContextCancels(t *testing.T) {
	ctx, stop := NotifyContext(context.Background(), syscall.SIGUSR1)
	defer stop()

	raise(t, syscall.SIGUSR1)
	assert.Eventually(t, func() bool { return ctx.Err() != nil }, arrive, tick)
}

func TestSuspendKeepsTheContext(t *testing.T) {
	ctx, stop := NotifyContext(context.Background(), syscall.SIGUSR1)
	defer stop()

	own := make(chan os.Signal, 1)
	signal.Notify(own, syscall.SIGUSR1)
	defer signal.Stop(own)

	restore := Suspend(ctx, syscall.SIGUSR1)

	raise(t, syscall.SIGUSR1)
	select {
	case <-own:
	case <-time.After(arrive):
		t.Fatal("the suspended signal never reached the private channel")
	}
	assert.Never(t, func() bool { return ctx.Err() != nil }, settle, tick)

	restore()

	raise(t, syscall.SIGUSR1)
	assert.Eventually(t, func() bool { return ctx.Err() != nil }, arrive, tick)
}

func TestSuspendLeavesOtherSignalsAlone(t *testing.T) {
	ctx, stop := NotifyContext(context.Background(), syscall.SIGUSR1, syscall.SIGUSR2)
	defer stop()

	restore := Suspend(ctx, syscall.SIGUSR1)
	defer restore()

	raise(t, syscall.SIGUSR2)
	assert.Eventually(t, func() bool { return ctx.Err() != nil }, arrive, tick)
}

func TestNestedSuspend(t *testing.T) {
	ctx, stop := NotifyContext(context.Background(), syscall.SIGUSR1)
	defer stop()

	own := make(chan os.Signal, 2)
	signal.Notify(own, syscall.SIGUSR1)
	defer signal.Stop(own)

	outer := Suspend(ctx, syscall.SIGUSR1)
	inner := Suspend(ctx, syscall.SIGUSR1)

	inner()
	raise(t, syscall.SIGUSR1)
	assert.Never(t, func() bool { return ctx.Err() != nil }, settle, tick)

	outer()
	raise(t, syscall.SIGUSR1)
	assert.Eventually(t, func() bool { return ctx.Err() != nil }, arrive, tick)
}

func TestSuspendWithoutARegistration(t *testing.T) {
	restore := Suspend(context.Background(), syscall.SIGUSR1)
	require.NotNil(t, restore)
	restore()
}
