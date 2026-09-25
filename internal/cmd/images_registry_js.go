// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package cmd

import (
	"context"

	"unikraft.com/cli/internal/resource"
)

type imageSpec = struct{}

func (i Image) Raw() any {
	return i.Ref.Reference.String()
}

func (Image) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	return nil, notAvailable("image get or delete")
}

func (Image) Delete(ctx context.Context, keys []string) error {
	return notAvailable("image delete")
}

func (cmd ImagesCopyCmd) Run(ctx context.Context, partition *resource.Partition) error {
	return notAvailable("image copy")
}
