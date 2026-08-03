// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/posener/complete"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
)

func PredictResourceKey[R resource.ListableResource](ctx context.Context) complete.Predictor {
	var empty R
	return predictResourceKey{
		ctx:          ctx,
		resourceType: empty,
	}
}

type predictResourceKey struct {
	ctx context.Context

	resourceType resource.ListableResource
}

func (p predictResourceKey) Predict(a complete.Args) []string {
	cfg, err := previewConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not load config for autocompletion:", err)
		return []string{}
	}
	if cfg == nil {
		return []string{}
	}

	ctx := config.WithConfig(p.ctx, cfg)

	resources, err := p.resourceType.List(ctx)
	if err != nil && len(resources) == 0 {
		return []string{}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: partial resource list for autocompletion:", err)
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

// previewConfig attempts to load the configuration based on the command line
// attempting to be completed. This is *very* basic, looking naively for --config
// and --profile flags, and using these to determine which configuration to load.
func previewConfig() (*config.Config, error) {
	line, _, ok := getEnv()
	if !ok {
		return nil, nil
	}
	a := newArgs(line)

	var cfg *config.Config
	var err error
	configPath, ok := lookupArg(a, "--config")
	if !ok {
		configPath, ok = os.LookupEnv("UNIKRAFT_CONFIG")
	}
	if !ok {
		configPath, err = config.ConfigFilePath()
		if err != nil {
			return nil, fmt.Errorf("getting config file path for completion: %w", err)
		}
	}
	cfg, err = config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config for completion: %w", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	profile, ok := lookupArg(a, "--profile")
	if !ok {
		profile, ok = os.LookupEnv("UNIKRAFT_PROFILE")
	}
	if ok {
		cfg.OverrideCurrentProfile(profile)
	}

	return cfg, nil
}

func lookupArg(args complete.Args, flag string) (string, bool) {
	for i, arg := range args.All {
		if arg == "--" {
			break
		}
		if arg == flag && i+1 < len(args.All) {
			return args.All[i+1], true
		}
		if strings.HasPrefix(arg, flag+"=") {
			return strings.SplitN(arg, "=", 2)[1], true
		}
	}
	return "", false
}
