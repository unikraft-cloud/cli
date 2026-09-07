// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package multimetro

import "unikraft.com/x/iata"

// normalizeIataCode returns the canonical form of an IATA code, or the code
// unchanged when it is not recognised.
func normalizeIataCode(code string) string {
	if matched := iata.ToIata(code); matched != iata.IataUnknown {
		return matched.Code
	}
	return code
}
