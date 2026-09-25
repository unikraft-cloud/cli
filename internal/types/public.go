// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package types

import "unikraft.com/cli/pkg/types"

type (
	DurationS  = types.DurationS
	DurationMS = types.DurationMS
	DurationUS = types.DurationUS
)

type (
	SizeBytes     = types.SizeBytes
	SizeMebibytes = types.SizeMebibytes
)

type RelativeTime = types.RelativeTime

func Now() RelativeTime {
	return types.Now()
}
