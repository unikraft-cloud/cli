// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"

	"github.com/posener/complete"

	"unikraft.com/cli/pkg/model"
)

func PredictModelKey[R model.ListableModel](ctx context.Context) complete.Predictor {
	var empty R
	return predictModelKey{
		ctx:          ctx,
		resourceType: empty,
	}
}

type predictModelKey struct {
	ctx context.Context

	resourceType model.ListableModel
}

func (p predictModelKey) Predict(a complete.Args) []string {
	resources, err := p.resourceType.List(p.ctx)
	if err != nil {
		return []string{}
	}

	completions := make([]string, 0, len(resources))
	for _, r := range resources {
		key := r.Key()
		if c, ok := key.(Completeable); ok {
			completions = append(completions, c.Complete(a.Last)...)
		} else {
			completions = append(completions, r.Key().String())
		}
	}
	return completions
}

type Completeable interface {
	Complete(prefix string) (completions []string)
}
