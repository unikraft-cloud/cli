// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package multimetro

// normalizeIataCode returns the code as the control plane reported it.  The
// lookup table behind the canonical form is too large for the wasm backend, and
// the web console reads its metro list from the console itself.
func normalizeIataCode(code string) string {
	return code
}
