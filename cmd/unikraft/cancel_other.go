// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !(js && wasm)

package main

import "context"

// installCancelHook does nothing outside wasm, where signals already deliver
// interrupts.  The returned function is a no-op.
func installCancelHook(cancel context.CancelFunc) func() {
	return func() {}
}
