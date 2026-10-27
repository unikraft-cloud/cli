// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/cmd"
	"unikraft.com/x/kingkong"
)

type ConfigCmd struct {
	cmd.ModelCmd[Config]
	cmd.GettableModelCmd[Config]
}

// Config wraps config.Config to implement the resource interfaces.
type Config struct {
	*config.Config
}

func (Config) Type() model.Type {
	return model.Type{
		Name:  "config",
		Names: "config",
	}
}

func (c Config) Key() model.Key {
	return staticKey(c.Path)
}

func (c Config) Raw() any {
	return c.Config
}

func (c Config) Fields(ctx context.Context) ([]model.Field, error) {
	return model.FieldsFromStruct(c.Config)
}

func (c Config) Default(ctx context.Context) (model.Model, error) {
	cfg := config.G(ctx)
	return Config{Config: cfg}, nil
}

func (Config) Get(ctx context.Context, keys []string) ([]model.Model, error) {
	if len(keys) == 0 {
		cfg := config.G(ctx)
		return []model.Model{Config{Config: cfg}}, nil
	}

	var (
		results []model.Model
		loadErr error
	)
	for _, key := range keys {
		cfg, err := config.Load(key)
		if err != nil {
			loadErr = errors.Join(loadErr, err)
			continue
		}
		if cfg == nil {
			loadErr = errors.Join(loadErr, fmt.Errorf("config file not found: %s", key))
			continue
		}
		results = append(results, Config{Config: cfg})
	}

	return results, loadErr
}

func (Config) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Show the current configuration",
				Commands:    []string{"unikraft config get"},
			},
		},
	}
}
