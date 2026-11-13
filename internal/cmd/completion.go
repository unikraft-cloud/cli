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

	kongcompletion "github.com/jotaen/kong-completion"
	"github.com/posener/complete"

	"unikraft.com/cli/internal/config"
)

// CompletionCmd wraps the kong-completion command.
type CompletionCmd struct {
	kongcompletion.Completion `embed:""`
}

// withCompletionConfig loads the config for the line being completed, if
// there is one. It naively looks for --config and --profile on it.
func withCompletionConfig(ctx context.Context) (context.Context, error) {
	line, _, ok := getEnv()
	if !ok {
		return ctx, nil
	}
	args := newArgs(line)

	configPath, ok := lookupArg(args, "--config")
	if !ok {
		configPath, ok = os.LookupEnv("UNIKRAFT_CONFIG")
	}
	if !ok {
		var err error
		configPath, err = config.ConfigFilePath()
		if err != nil {
			return nil, fmt.Errorf("getting config file path: %w", err)
		}
	}
	profile, ok := lookupArg(args, "--profile")
	if !ok {
		profile = os.Getenv("UNIKRAFT_PROFILE")
	}
	cfg, err := loadConfig(configPath, profile)
	if err != nil {
		return nil, err
	}
	return config.WithConfig(ctx, cfg), nil
}

// lookupArg returns the value of flag in args, as either "--flag value" or
// "--flag=value".
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
