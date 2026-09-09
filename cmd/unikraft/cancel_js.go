// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js && wasm

package main

import (
	"context"
	"syscall/js"
)

// installCancelHook publishes a global "unikraftCancel" function so the host
// page can interrupt the running command.  Signals never arrive in wasm, so
// this takes the place of Ctrl-C.  The returned function removes the hook.
func installCancelHook(cancel context.CancelFunc) func() {
	fn := js.FuncOf(func(js.Value, []js.Value) any {
		cancel()
		return nil
	})
	js.Global().Set("unikraftCancel", fn)

	return func() {
		js.Global().Delete("unikraftCancel")
		fn.Release()
	}
}
