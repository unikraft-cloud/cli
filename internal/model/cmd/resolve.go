// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"

	"unikraft.com/cli/internal/model"
	"unikraft.com/x/joinerrgroup"
)

type resolvedModel struct {
	model.Model
	fields []model.Field
}

func (r resolvedModel) Fields(ctx context.Context) ([]model.Field, error) {
	return model.CloneFields(r.fields), nil
}

func resolveModels(ctx context.Context, resources []model.Model, paths []model.FieldPath) ([]model.Model, error) {
	if len(resources) == 0 {
		return resources, nil
	}

	resolved := make([]model.Model, len(resources))
	eg := joinerrgroup.Group{}
	for i, res := range resources {
		eg.Go(func() error {
			resolvedRes, err := resolveModel(ctx, res, paths)
			if err != nil {
				return err
			}
			resolved[i] = resolvedRes
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return resolved, nil
}

func resolveModel(ctx context.Context, res model.Model, paths []model.FieldPath) (model.Model, error) {
	fields, err := res.Fields(ctx)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return res, nil
	}

	resolved := model.CloneFields(fields)
	if err := model.ResolveFields(ctx, resolved, paths); err != nil {
		return nil, err
	}

	return resolvedModel{Model: res, fields: resolved}, nil
}

func resolveAllModels(ctx context.Context, resources []model.Model) ([]model.Model, error) {
	if len(resources) == 0 {
		return resources, nil
	}

	resolved := make([]model.Model, len(resources))
	eg := joinerrgroup.Group{}
	for i, res := range resources {
		eg.Go(func() error {
			resolvedRes, err := resolveAllModel(ctx, res)
			if err != nil {
				return err
			}
			resolved[i] = resolvedRes
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return resolved, nil
}

func resolveAllModel(ctx context.Context, res model.Model) (model.Model, error) {
	fields, err := res.Fields(ctx)
	if err != nil {
		return nil, err
	}

	resolved := model.CloneFields(fields)
	if err := model.ResolveAllFields(ctx, resolved); err != nil {
		return nil, err
	}

	return resolvedModel{Model: res, fields: resolved}, nil
}

func unwrapModel(r model.Model) model.Model {
	if rr, ok := r.(resolvedModel); ok {
		return rr.Model
	}
	return r
}

func unwrapModels(resources []model.Model) []model.Model {
	result := make([]model.Model, len(resources))
	for i, r := range resources {
		result[i] = unwrapModel(r)
	}
	return result
}
