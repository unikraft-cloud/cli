// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"cmp"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultModel       = "claude-sonnet-5-5"
	defaultReps        = 1
	defaultConcurrency = 4
	defaultTimeout     = 3 * time.Minute
	maxTurns           = 12
)

// Config holds the settings of an eval run, read from the environment by
// LoadConfig.
type Config struct {
	// Model is the model under test, UNIKRAFT_EVAL_MODEL.
	Model string
	// Reps is how many times each prompt runs, UNIKRAFT_EVAL_REPS.
	Reps int
	// OutDir receives results.jsonl and the traces, UNIKRAFT_EVAL_OUT.
	OutDir string
	// Effort is the model's effort level, UNIKRAFT_EVAL_EFFORT; empty keeps
	// the API default.
	Effort string
	// Concurrency bounds the model requests in flight, UNIKRAFT_EVAL_CONCURRENCY.
	Concurrency int
	// Timeout bounds one run of a prompt, UNIKRAFT_EVAL_TIMEOUT.
	Timeout time.Duration
	// MaxTurns bounds the model turns of one run of a prompt.
	MaxTurns int
}

func LoadConfig() (Config, error) {
	cfg := Config{
		Model:    cmp.Or(os.Getenv("UNIKRAFT_EVAL_MODEL"), defaultModel),
		OutDir:   os.Getenv("UNIKRAFT_EVAL_OUT"),
		Effort:   os.Getenv("UNIKRAFT_EVAL_EFFORT"),
		MaxTurns: maxTurns,
	}
	var err error
	if cfg.Reps, err = envInt("UNIKRAFT_EVAL_REPS", defaultReps); err != nil {
		return cfg, err
	}
	if cfg.Concurrency, err = envInt("UNIKRAFT_EVAL_CONCURRENCY", defaultConcurrency); err != nil {
		return cfg, err
	}
	if cfg.Timeout, err = envDuration("UNIKRAFT_EVAL_TIMEOUT", defaultTimeout); err != nil {
		return cfg, err
	}
	if cfg.OutDir == "" {
		if cfg.OutDir, err = os.MkdirTemp("", "unikraft-eval-*"); err != nil {
			return cfg, err
		}
	} else if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, value)
	}
	return n, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration, got %q", name, value)
	}
	return d, nil
}
