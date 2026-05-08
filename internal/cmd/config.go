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
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/x/kingkong"
)

type ConfigCmd struct {
	cmd.ResourceCmd[Config]
	cmd.GettableResourceCmd[Config]
}

// Config wraps config.Config to implement the resource interfaces.
type Config struct {
	*config.Config
}

func (Config) Type() resource.Type {
	return resource.Type{
		Name:  "config",
		Names: "config",
	}
}

func (c Config) Key() resource.Key {
	return staticKey(c.Path)
}

func (c Config) Raw() any {
	return c.Config
}

func (c Config) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(c.Config)
}

func (c Config) Default(ctx context.Context) (resource.Resource, error) {
	cfg := config.G(ctx)
	return Config{Config: cfg}, nil
}

func (Config) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	if len(keys) == 0 {
		cfg := config.G(ctx)
		return []resource.Resource{Config{Config: cfg}}, nil
	}

	var (
		results []resource.Resource
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
			{
				Description: "Show config from a specific file",
				Commands:    []string{"unikraft config get /path/to/config.yaml"},
			},
			{
				Description: "Show a specific field in JSON format",
				Commands:    []string{"unikraft config get -f profiles -o json"},
			},
		},
	}
}
