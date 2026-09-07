// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package main

import (
	"cmp"
	"context"
	"os"
	"syscall/js"
)

// installCancelHook sets a global function that stops the command. Its name is
// UNIKRAFT_CANCEL_HOOK, or "unikraftCancel" by default.
func installCancelHook(cancel context.CancelFunc) func() {
	name := cmp.Or(os.Getenv("UNIKRAFT_CANCEL_HOOK"), "unikraftCancel")
	fn := js.FuncOf(func(js.Value, []js.Value) any {
		cancel()
		return nil
	})
	js.Global().Set(name, fn)

	return func() {
		if js.Global().Get(name).Equal(fn.Value) {
			js.Global().Delete(name)
		}
		fn.Release()
	}
}
